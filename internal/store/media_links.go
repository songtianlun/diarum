package store

import (
	"regexp"
	"slices"
)

// mediaReferencePattern matches the built-in media file URLs the editor
// inserts, capturing the media ID. Legacy PocketBase URLs from older entries
// (/api/files/[media/]<collectionId>/<id>/...) match too.
var mediaReferencePattern = regexp.MustCompile(`/api/(?:v1/files/media|files/(?:media/)?[A-Za-z0-9_]+)/([A-Za-z0-9_-]+)/`)

// ReferencedMediaIDs returns the distinct built-in media IDs referenced by the
// given diary HTML, in order of first appearance.
func ReferencedMediaIDs(content string) []string {
	matches := mediaReferencePattern.FindAllStringSubmatch(content, -1)
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		if !slices.Contains(ids, match[1]) {
			ids = append(ids, match[1])
		}
	}
	return ids
}

// LinkDiaryMedia adds diaryID to every media record of owner that the diary
// content references. Links are only ever added: removing an image from an
// entry keeps the association, matching how uploads were always tracked.
// Doing this on save means uploads never need to create the entry up front.
//
// An image gets its date from the first entry it is linked to. An image that
// went to the trash because no entry used it comes back out when an entry is
// saved with it again; one deleted by hand stays in the trash.
func (s *Store) LinkDiaryMedia(owner, diaryID, content string) error {
	for _, id := range ReferencedMediaIDs(content) {
		media, err := s.GetMedia(id, owner)
		if err != nil {
			continue
		}
		if media.InTrash() {
			if media.DeleteReason != MediaReasonUnlinked {
				continue
			}
			restored, err := s.RestoreMedia(media.ID, owner)
			if err != nil {
				continue
			}
			if s.MediaEvent != nil {
				s.MediaEvent("media.restore", restored, map[string]any{"diary": diaryID, "reason": MediaReasonReferenced})
			}
			media = restored
		}
		if err := s.setMediaDateIfEmpty(media, diaryID); err != nil {
			return err
		}
		if slices.Contains(media.Diary, diaryID) {
			continue
		}
		if _, err := s.UpdateMediaDiary(media.ID, owner, append(media.Diary, diaryID)); err != nil {
			return err
		}
	}
	return nil
}
