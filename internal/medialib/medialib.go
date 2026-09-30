// Package medialib runs the housekeeping of the built-in media library: it
// empties the trash of images kept past each user's retention period and,
// for users who turned it on, moves images no entry uses any more to the
// trash. Every change is written to the audit trail as an automatic action.
package medialib

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

// Setting keys, registered in internal/config.
const (
	SettingTrashRetentionDays = "media.trash_retention_days"
	SettingAutoCleanUnlinked  = "media.auto_clean_unlinked"
)

const (
	// DefaultTrashRetentionDays is how long images stay in the trash unless
	// the user chose otherwise; 0 keeps them until removed by hand.
	DefaultTrashRetentionDays = 30
	MaxTrashRetentionDays     = 3650

	// ManualScanGrace and AutoCleanGrace skip images uploaded this recently
	// when looking for unused ones: their entry may not be saved yet.
	ManualScanGrace = 10 * time.Minute
	AutoCleanGrace  = 24 * time.Hour

	runInterval  = time.Hour
	startupDelay = 2 * time.Minute
)

// Settings are a user's media library housekeeping preferences.
type Settings struct {
	// TrashRetentionDays: images older than this in the trash are removed
	// for good; 0 never removes them automatically.
	TrashRetentionDays int  `json:"trash_retention_days"`
	AutoCleanUnlinked  bool `json:"auto_clean_unlinked"`
}

// Service performs housekeeping runs, on a schedule and on demand.
type Service struct {
	store *store.Store
	audit *audit.Logger
	now   func() time.Time

	// running keeps two runs from working on the same user at once.
	mu      sync.Mutex
	running map[string]bool
}

// New creates the service. A nil audit logger records nothing.
func New(s *store.Store, auditLog *audit.Logger) *Service {
	return &Service{store: s, audit: auditLog, now: time.Now, running: make(map[string]bool)}
}

// LoadSettings reads a user's housekeeping settings, with defaults.
func (svc *Service) LoadSettings(userID string) Settings {
	settings := Settings{TrashRetentionDays: DefaultTrashRetentionDays}
	if value, err := svc.store.GetSetting(userID, SettingTrashRetentionDays); err == nil {
		if days, ok := toInt(value); ok && days >= 0 && days <= MaxTrashRetentionDays {
			settings.TrashRetentionDays = days
		}
	}
	if value, err := svc.store.GetSetting(userID, SettingAutoCleanUnlinked); err == nil {
		settings.AutoCleanUnlinked, _ = value.(bool)
	}
	return settings
}

func toInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), v == float64(int(v))
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// Start runs housekeeping shortly after startup and then every hour, until
// ctx ends.
func (svc *Service) Start(ctx context.Context) {
	go func() {
		timer := time.NewTimer(startupDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			svc.RunAll()
			timer.Reset(runInterval)
		}
	}()
}

// RunAll does one housekeeping pass over every user with images.
func (svc *Service) RunAll() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[MediaLib] housekeeping panicked: %v", r)
		}
	}()
	if err := svc.store.BackfillMediaDates(); err != nil {
		logger.Warn("[MediaLib] image date backfill failed: %v", err)
	}
	owners, err := svc.store.MediaOwners()
	if err != nil {
		logger.Error("[MediaLib] could not list media owners: %v", err)
		return
	}
	for _, owner := range owners {
		svc.RunUser(owner)
	}
}

// Result counts what one run did.
type Result struct {
	Trashed int
	Purged  int
	Failed  int
}

// RunUser does one housekeeping pass for a user: unused images go to the
// trash (if enabled), then images past the retention period are purged.
func (svc *Service) RunUser(userID string) Result {
	var result Result
	if !svc.begin(userID) {
		return result
	}
	defer svc.end(userID)

	settings := svc.LoadSettings(userID)
	actor := svc.username(userID)
	now := svc.now()

	if settings.AutoCleanUnlinked {
		unlinked, err := svc.store.UnlinkedMedia(userID, now.Add(-AutoCleanGrace))
		if err != nil {
			logger.Error("[MediaLib] unused image scan for %s failed: %v", userID, err)
		}
		for _, media := range unlinked {
			trashed, err := svc.store.TrashMedia(media.ID, userID, store.TrashInfo{By: "system", Reason: store.MediaReasonUnlinked, Trigger: store.MediaTriggerAuto})
			if err != nil {
				if !store.IsNoRows(err) {
					result.Failed++
					logger.Error("[MediaLib] moving unused image %s to the trash failed: %v", media.ID, err)
				}
				continue
			}
			result.Trashed++
			svc.record(userID, actor, audit.ActionMediaTrash, trashed, store.MediaReasonUnlinked, nil)
		}
	}

	if settings.TrashRetentionDays > 0 {
		cutoff := now.AddDate(0, 0, -settings.TrashRetentionDays)
		expired, err := svc.store.TrashedMedia(userID, cutoff)
		if err != nil {
			logger.Error("[MediaLib] listing expired trash of %s failed: %v", userID, err)
		}
		for _, media := range expired {
			purged, err := svc.store.PurgeMedia(media.ID, userID)
			if err != nil {
				if store.IsNoRows(err) || errors.Is(err, store.ErrMediaBusy) {
					continue
				}
				result.Failed++
				logger.Error("[MediaLib] purging image %s failed, will retry: %v", media.ID, err)
				svc.record(userID, actor, audit.ActionMediaPurge, media, store.MediaReasonRetention, map[string]any{"error": err.Error()})
				continue
			}
			result.Purged++
			svc.record(userID, actor, audit.ActionMediaPurge, purged, store.MediaReasonRetention, map[string]any{"retention_days": settings.TrashRetentionDays})
		}
	}

	if result.Trashed+result.Purged+result.Failed > 0 {
		logger.Info("[MediaLib] housekeeping for %s: trashed=%d purged=%d failed=%d", userID, result.Trashed, result.Purged, result.Failed)
	}
	return result
}

// OnStoreEvent records media changes the store makes on its own, such as a
// trashed image restored because an entry uses it again. Wire it to
// store.Store.MediaEvent.
func (svc *Service) OnStoreEvent(action string, media *store.Media, detail map[string]any) {
	if media == nil {
		return
	}
	reason, _ := detail["reason"].(string)
	extra := map[string]any{}
	for k, v := range detail {
		if k != "reason" {
			extra[k] = v
		}
	}
	svc.record(media.Owner, svc.username(media.Owner), action, media, reason, extra)
}

func (svc *Service) begin(userID string) bool {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.running[userID] {
		return false
	}
	svc.running[userID] = true
	return true
}

func (svc *Service) end(userID string) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	delete(svc.running, userID)
}

func (svc *Service) username(userID string) string {
	if user, err := svc.store.GetUserByID(userID); err == nil {
		return user.Username
	}
	return ""
}

// record writes an automatic media action to the audit trail.
func (svc *Service) record(userID, username, action string, media *store.Media, reason string, extra map[string]any) {
	detail := Detail(media, store.MediaTriggerAuto, reason)
	for k, v := range extra {
		detail[k] = v
	}
	svc.audit.Record(audit.Entry{
		UserID: userID,
		User:   username,
		Source: audit.SourceSystem,
		Action: action,
		Target: media.Name,
		Detail: detail,
	})
}

// Detail describes an image for an audit entry: which image, its dates and
// where it lives, plus whether a person or Diarum acted, and why.
func Detail(media *store.Media, trigger, reason string) map[string]any {
	detail := map[string]any{
		"id":      media.ID,
		"file":    media.File,
		"trigger": trigger,
	}
	if reason != "" {
		detail["reason"] = reason
	}
	if media.Date != "" {
		detail["image_date"] = store.DateOnly(media.Date)
	}
	if media.Created != "" {
		detail["uploaded"] = media.Created
	}
	if media.Storage != "" {
		detail["storage"] = media.Storage
	}
	if media.Deleted != "" {
		detail["trashed"] = media.Deleted
		if media.DeletedBy != "" {
			detail["trashed_by"] = media.DeletedBy
		}
	}
	return detail
}
