package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	dayNameMaxRunes  = 100
	dayNoteMaxRunes  = 2000
	dayEmojiMaxBytes = 32
)

type dayRequest struct {
	Name         string `json:"name"`
	Date         string `json:"date"`
	Emoji        string `json:"emoji"`
	Note         string `json:"note"`
	Repeat       string `json:"repeat"`
	IncludeStart bool   `json:"include_start"`
	ShowOnHome   bool   `json:"show_on_home"`
}

type milestoneRequest struct {
	Name string `json:"name"`
	Date string `json:"date"`
	Note string `json:"note"`
}

// RegisterDayRoutes serves countdowns and anniversaries ("days") and their
// milestones.
func RegisterDayRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc) {
	group := e.Group("/api/v1/days", authMiddleware)

	group.GET("", func(c echo.Context) error {
		days, err := s.ListDays(auth.CurrentUser(c).ID)
		if err != nil {
			return serverError("Failed to list days", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"days": days})
	})

	group.POST("", func(c echo.Context) error {
		day, err := bindDay(c)
		if err != nil {
			return err
		}
		saved, err := s.SaveDay(day)
		if err != nil {
			return serverError("Failed to save day", err)
		}
		return c.JSON(http.StatusOK, saved)
	})

	group.GET("/:id", func(c echo.Context) error {
		owner := auth.CurrentUser(c).ID
		day, err := s.GetDay(owner, c.PathParam("id"))
		if err != nil {
			return dayError(err, "Failed to load day")
		}
		milestones, err := s.ListDayMilestones(owner, day.ID)
		if err != nil {
			return serverError("Failed to list milestones", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"day": day, "milestones": milestones})
	})

	group.PUT("/:id", func(c echo.Context) error {
		day, err := bindDay(c)
		if err != nil {
			return err
		}
		day.ID = c.PathParam("id")
		saved, err := s.SaveDay(day)
		if err != nil {
			return dayError(err, "Failed to save day")
		}
		return c.JSON(http.StatusOK, saved)
	})

	group.DELETE("/:id", func(c echo.Context) error {
		if err := s.DeleteDay(auth.CurrentUser(c).ID, c.PathParam("id")); err != nil {
			return dayError(err, "Failed to delete day")
		}
		return c.NoContent(http.StatusNoContent)
	})

	group.POST("/:id/milestones", func(c echo.Context) error {
		return saveMilestone(c, s, "")
	})

	group.PUT("/:id/milestones/:mid", func(c echo.Context) error {
		return saveMilestone(c, s, c.PathParam("mid"))
	})

	group.DELETE("/:id/milestones/:mid", func(c echo.Context) error {
		if err := s.DeleteDayMilestone(auth.CurrentUser(c).ID, c.PathParam("id"), c.PathParam("mid")); err != nil {
			return dayError(err, "Failed to delete milestone")
		}
		return c.NoContent(http.StatusNoContent)
	})
}

func bindDay(c echo.Context) (*store.Day, error) {
	var body dayRequest
	if err := c.Bind(&body); err != nil {
		return nil, badRequest("Invalid request body", err)
	}
	name, date, note, err := validateDated(body.Name, body.Date, body.Note)
	if err != nil {
		return nil, err
	}
	switch body.Repeat {
	case store.DayRepeatNone, store.DayRepeatWeek, store.DayRepeatMonth, store.DayRepeatYear:
	default:
		return nil, badRequest("repeat must be empty, week, month or year", nil)
	}
	emoji := strings.TrimSpace(body.Emoji)
	if len(emoji) > dayEmojiMaxBytes {
		return nil, badRequest("emoji is too long", nil)
	}
	return &store.Day{
		Owner:        auth.CurrentUser(c).ID,
		Name:         name,
		Date:         date,
		Emoji:        emoji,
		Note:         note,
		Repeat:       body.Repeat,
		IncludeStart: body.IncludeStart,
		ShowOnHome:   body.ShowOnHome,
	}, nil
}

func saveMilestone(c echo.Context, s *store.Store, id string) error {
	var body milestoneRequest
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	name, date, note, err := validateDated(body.Name, body.Date, body.Note)
	if err != nil {
		return err
	}
	saved, err := s.SaveDayMilestone(&store.DayMilestone{
		ID:    id,
		Day:   c.PathParam("id"),
		Owner: auth.CurrentUser(c).ID,
		Name:  name,
		Date:  date,
		Note:  note,
	})
	if err != nil {
		return dayError(err, "Failed to save milestone")
	}
	return c.JSON(http.StatusOK, saved)
}

// validateDated checks the fields days and milestones share and returns them
// trimmed.
func validateDated(name, date, note string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", badRequest("name is required", nil)
	}
	if utf8.RuneCountInString(name) > dayNameMaxRunes {
		return "", "", "", badRequest("name is too long", nil)
	}
	date = strings.TrimSpace(date)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", "", "", badRequest("date must be YYYY-MM-DD", nil)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > dayNoteMaxRunes {
		return "", "", "", badRequest("note is too long", nil)
	}
	return name, date, note, nil
}

func dayError(err error, message string) error {
	if errors.Is(err, store.ErrNotFound) {
		return notFound("Day not found")
	}
	return serverError(message, err)
}
