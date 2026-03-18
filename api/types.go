package api

import (
	"encoding/json"
	"fmt"
)

type Person struct {
	ID    int    `json:"id"`
	URL   string `json:"url"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type User struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type ProjectSummary struct {
	ID                   int     `json:"id"`
	URL                  string  `json:"url"`
	Name                 string  `json:"name"`
	LinkName             string  `json:"link_name"`
	ListID               string  `json:"list_id"`
	ListEmail            string  `json:"list_email"`
	WebURL               *string `json:"web_url"`
	ScmURL               *string `json:"scm_url"`
	WebScmURL            *string `json:"webscm_url"`
	ListArchiveURL       *string `json:"list_archive_url"`
	ListArchiveURLFormat *string `json:"list_archive_url_format"`
	CommitURLFormat      string  `json:"commit_url_format"`
}

type Project struct {
	ProjectSummary
	Maintainers  []User `json:"maintainers"`
	SubjectMatch string `json:"subject_match"`
}

type SeriesSummary struct {
	ID      int    `json:"id"`
	URL     string `json:"url"`
	WebURL  string `json:"web_url"`
	Name    string `json:"name"`
	Date    string `json:"date"`
	Version int    `json:"version"`
	Mbox    string `json:"mbox"`
}

type PatchSummary struct {
	ID             int     `json:"id"`
	URL            string  `json:"url"`
	WebURL         string  `json:"web_url"`
	MsgID          string  `json:"msgid"`
	ListArchiveURL *string `json:"list_archive_url"`
	Date           string  `json:"date"`
	Name           string  `json:"name"`
	Mbox           string  `json:"mbox"`
}

type CoverSummary = PatchSummary

type CommentSummary struct {
	ID             int     `json:"id"`
	URL            string  `json:"url"`
	WebURL         string  `json:"web_url"`
	MsgID          string  `json:"msgid"`
	ListArchiveURL *string `json:"list_archive_url"`
	Date           string  `json:"date"`
}

type CheckSummary struct {
	ID        int     `json:"id"`
	URL       string  `json:"url"`
	Date      string  `json:"date"`
	State     string  `json:"state"`
	TargetURL *string `json:"target_url"`
	Context   string  `json:"context"`
}

type Patch struct {
	ID             int             `json:"id"`
	URL            string          `json:"url"`
	WebURL         string          `json:"web_url"`
	Project        ProjectSummary  `json:"project"`
	MsgID          string          `json:"msgid"`
	ListArchiveURL *string         `json:"list_archive_url"`
	Date           string          `json:"date"`
	Name           string          `json:"name"`
	CommitRef      *string         `json:"commit_ref"`
	PullURL        *string         `json:"pull_url"`
	State          string          `json:"state"`
	Archived       bool            `json:"archived"`
	Hash           *string         `json:"hash"`
	Submitter      Person          `json:"submitter"`
	Delegate       *User           `json:"delegate"`
	Mbox           string          `json:"mbox"`
	Series         []SeriesSummary `json:"series"`
	Comments       string          `json:"comments"`
	Check          string          `json:"check"`
	Checks         string          `json:"checks"`
	Tags           map[string]int  `json:"tags"`
	Related        []PatchSummary  `json:"related"`
}

type PatchDetail struct {
	Patch
	Headers  map[string]interface{} `json:"headers"`
	Content  string                 `json:"content"`
	Diff     string                 `json:"diff"`
	Prefixes []string               `json:"prefixes"`
}

type Series struct {
	ID            int            `json:"id"`
	URL           string         `json:"url"`
	WebURL        string         `json:"web_url"`
	Project       ProjectSummary `json:"project"`
	Name          string         `json:"name"`
	Date          string         `json:"date"`
	Submitter     Person         `json:"submitter"`
	Version       int            `json:"version"`
	Total         int            `json:"total"`
	ReceivedTotal int            `json:"received_total"`
	ReceivedAll   bool           `json:"received_all"`
	Mbox          string         `json:"mbox"`
	CoverLetter   *PatchSummary  `json:"cover_letter"`
	Patches       []PatchSummary `json:"patches"`
}

type Cover struct {
	ID             int             `json:"id"`
	URL            string          `json:"url"`
	WebURL         string          `json:"web_url"`
	Project        ProjectSummary  `json:"project"`
	MsgID          string          `json:"msgid"`
	ListArchiveURL *string         `json:"list_archive_url"`
	Date           string          `json:"date"`
	Name           string          `json:"name"`
	Submitter      Person          `json:"submitter"`
	Mbox           string          `json:"mbox"`
	Series         []SeriesSummary `json:"series"`
	Comments       string          `json:"comments"`
}

type CoverDetail struct {
	Cover
	Headers map[string]interface{} `json:"headers"`
	Content string                 `json:"content"`
}

type Check struct {
	ID          int     `json:"id"`
	URL         string  `json:"url"`
	User        User    `json:"user"`
	Date        string  `json:"date"`
	State       string  `json:"state"`
	TargetURL   *string `json:"target_url"`
	Context     string  `json:"context"`
	Description *string `json:"description"`
}

type Comment struct {
	ID             int                    `json:"id"`
	URL            string                 `json:"url"`
	WebURL         string                 `json:"web_url"`
	MsgID          string                 `json:"msgid"`
	ListArchiveURL *string                `json:"list_archive_url"`
	Date           string                 `json:"date"`
	Subject        string                 `json:"subject"`
	Submitter      Person                 `json:"submitter"`
	Content        string                 `json:"content"`
	Headers        map[string]interface{} `json:"headers"`
}

type EventPayload interface {
	eventPayload()
}

type Event struct {
	ID       int            `json:"id"`
	Category string         `json:"category"`
	Project  ProjectSummary `json:"project"`
	Date     string         `json:"date"`
	Actor    *User          `json:"actor"`
	Payload  EventPayload   `json:"-"`
}

type CoverCreatedPayload struct {
	Cover CoverSummary `json:"cover"`
}

type PatchCreatedPayload struct {
	Patch PatchSummary `json:"patch"`
}

type PatchCompletedPayload struct {
	Patch  PatchSummary  `json:"patch"`
	Series SeriesSummary `json:"series"`
}

type PatchStateChangedPayload struct {
	Patch         PatchSummary `json:"patch"`
	PreviousState string       `json:"previous_state"`
	CurrentState  string       `json:"current_state"`
}

type PatchDelegatedPayload struct {
	Patch            PatchSummary `json:"patch"`
	PreviousDelegate *User        `json:"previous_delegate"`
	CurrentDelegate  *User        `json:"current_delegate"`
}

type PatchRelationChangedPayload struct {
	Patch            PatchSummary `json:"patch"`
	PreviousRelation *string      `json:"previous_relation"`
	CurrentRelation  *string      `json:"current_relation"`
}

type CheckCreatedPayload struct {
	Patch PatchSummary `json:"patch"`
	Check CheckSummary `json:"check"`
}

type SeriesCreatedPayload struct {
	Series SeriesSummary `json:"series"`
}

type SeriesCompletedPayload struct {
	Series SeriesSummary `json:"series"`
}

type PatchCommentCreatedPayload struct {
	Patch   PatchSummary   `json:"patch"`
	Comment CommentSummary `json:"comment"`
}

type CoverCommentCreatedPayload struct {
	Cover   CoverSummary   `json:"cover"`
	Comment CommentSummary `json:"comment"`
}

func (*CoverCreatedPayload) eventPayload()         {}
func (*PatchCreatedPayload) eventPayload()         {}
func (*PatchCompletedPayload) eventPayload()       {}
func (*PatchStateChangedPayload) eventPayload()    {}
func (*PatchDelegatedPayload) eventPayload()       {}
func (*PatchRelationChangedPayload) eventPayload() {}
func (*CheckCreatedPayload) eventPayload()         {}
func (*SeriesCreatedPayload) eventPayload()        {}
func (*SeriesCompletedPayload) eventPayload()      {}
func (*PatchCommentCreatedPayload) eventPayload()  {}
func (*CoverCommentCreatedPayload) eventPayload()  {}

func (e *Event) UnmarshalJSON(data []byte) error {
	// Unmarshal common fields first
	type eventAlias Event
	var raw struct {
		eventAlias
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal event: %w", err)
	}

	*e = Event(raw.eventAlias)

	if len(raw.Payload) == 0 {
		return nil
	}

	var payload EventPayload
	switch e.Category {
	case "cover-created":
		payload = new(CoverCreatedPayload)
	case "patch-created":
		payload = new(PatchCreatedPayload)
	case "patch-completed":
		payload = new(PatchCompletedPayload)
	case "patch-state-changed":
		payload = new(PatchStateChangedPayload)
	case "patch-delegated":
		payload = new(PatchDelegatedPayload)
	case "patch-relation-changed":
		payload = new(PatchRelationChangedPayload)
	case "check-created":
		payload = new(CheckCreatedPayload)
	case "series-created":
		payload = new(SeriesCreatedPayload)
	case "series-completed":
		payload = new(SeriesCompletedPayload)
	case "patch-comment-created":
		payload = new(PatchCommentCreatedPayload)
	case "cover-comment-created":
		payload = new(CoverCommentCreatedPayload)
	default:
		return nil
	}

	if err := json.Unmarshal(raw.Payload, payload); err != nil {
		return fmt.Errorf("unmarshal %s payload: %w", e.Category, err)
	}

	e.Payload = payload
	return nil
}
