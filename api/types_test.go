package api

import (
	"encoding/json"
	"testing"
)

const testProjectJSON = `{
	"id":1, "url":"", "name":"test",
	"link_name":"test", "list_id":"",
	"list_email":"", "web_url":"",
	"scm_url":"", "webscm_url":"",
	"list_archive_url":"",
	"list_archive_url_format":"",
	"commit_url_format":""
}`

func TestUnmarshalPerson(t *testing.T) {
	raw := `{
		"id": 10471,
		"url": "https://pw.example.com/api/1.2/people/10471/",
		"name": "Lorem Ipsum",
		"email": "lorem@ipsum.example"
	}`
	var p Person
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.ID != 10471 {
		t.Errorf("ID = %d", p.ID)
	}
	if p.Name != "Lorem Ipsum" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Email != "lorem@ipsum.example" {
		t.Errorf("Email = %q", p.Email)
	}
}

func TestUnmarshalUser(t *testing.T) {
	raw := `{
		"id": 51847,
		"url": "https://pw.example.com/api/1.2/users/51847/",
		"username": "lorem",
		"first_name": "Lorem",
		"last_name": "Ipsum",
		"email": "lorem@ipsum.example"
	}`
	var u User
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.ID != 51847 {
		t.Errorf("ID = %d", u.ID)
	}
	if u.Username != "lorem" {
		t.Errorf("Username = %q", u.Username)
	}
	if u.FirstName != "Lorem" {
		t.Errorf("FirstName = %q", u.FirstName)
	}
	if u.LastName != "Ipsum" {
		t.Errorf("LastName = %q", u.LastName)
	}
}

func TestUnmarshalPatch_RealOzlabs(t *testing.T) {
	raw := `{
		"id": 512232,
		"url": "https://pw.example.com/api/1.2/patches/512232/",
		"web_url": "https://pw.example.com/project/example-project/patch/1440945631-11972-1-lorem@ipsum.example/",
		"project": {
			"id": 47,
			"url": "https://pw.example.com/api/1.2/projects/47/",
			"name": "Example Project",
			"link_name": "example-project",
			"list_id": "dev.example.com",
			"list_email": "dev@example.com",
			"web_url": "https://www.example.com/",
			"scm_url": "git@git.example.com:example-project/repo.git",
			"webscm_url": "https://git.example.com/example-project/repo",
			"list_archive_url": "",
			"list_archive_url_format": "",
			"commit_url_format": ""
		},
		"msgid": "<lorem-001@ipsum.example>",
		"list_archive_url": null,
		"date": "2015-08-30T14:40:31",
		"name": "[dev] Lorem ipsum dolor sit amet",
		"commit_ref": null,
		"pull_url": null,
		"state": "accepted",
		"archived": false,
		"hash": "25f025f83ba33cbef32475295bf3da2984aac746",
		"submitter": {
			"id": 10471,
			"url": "https://pw.example.com/api/1.2/people/10471/",
			"name": "Lorem Ipsum",
			"email": "lorem@ipsum.example"
		},
		"delegate": null,
		"mbox": "https://pw.example.com/project/example-project/patch/1440945631-11972-1-lorem@ipsum.example/mbox/",
		"series": [],
		"comments": "https://pw.example.com/api/patches/512232/comments/",
		"check": "pending",
		"checks": "https://pw.example.com/api/patches/512232/checks/",
		"tags": {},
		"related": []
	}`
	var p Patch
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.ID != 512232 {
		t.Errorf("ID = %d", p.ID)
	}
	if p.Name != "[dev] Lorem ipsum dolor sit amet" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.State != "accepted" {
		t.Errorf("State = %q", p.State)
	}
	if p.MsgID != "<lorem-001@ipsum.example>" {
		t.Errorf("MsgID = %q", p.MsgID)
	}
	if p.CommitRef != nil {
		t.Errorf("CommitRef = %v, want nil", p.CommitRef)
	}
	if p.Delegate != nil {
		t.Errorf("Delegate = %v, want nil", p.Delegate)
	}
	if p.Submitter.Name != "Lorem Ipsum" {
		t.Errorf("Submitter.Name = %q", p.Submitter.Name)
	}
	if len(p.Series) != 0 {
		t.Errorf("Series = %v, want empty", p.Series)
	}
	wantComments := "https://pw.example.com/api/patches/512232/comments/"
	if p.Comments != wantComments {
		t.Errorf("Comments = %q", p.Comments)
	}
	wantChecks := "https://pw.example.com/api/patches/512232/checks/"
	if p.Checks != wantChecks {
		t.Errorf("Checks = %q", p.Checks)
	}
	if p.Check != "pending" {
		t.Errorf("Check = %q", p.Check)
	}
	if len(p.Related) != 0 {
		t.Errorf("Related = %v, want empty", p.Related)
	}
	if p.Project.LinkName != "example-project" {
		t.Errorf("Project.LinkName = %q",
			p.Project.LinkName)
	}
	hash := "25f025f83ba33cbef32475295bf3da2984aac746"
	if p.Hash == nil || *p.Hash != hash {
		t.Errorf("Hash = %v, want %q", p.Hash, hash)
	}
}

func TestUnmarshalPatch_WithDelegate(t *testing.T) {
	raw := `{
		"id": 2205820,
		"url": "https://pw.example.com/api/1.2/patches/2205820/",
		"web_url": "",
		"project": {
			"id":47, "url":"",
			"name":"Example Project",
			"link_name":"example-project",
			"list_id":"", "list_email":"",
			"web_url":"", "scm_url":"",
			"webscm_url":"",
			"list_archive_url":"",
			"list_archive_url_format":"",
			"commit_url_format":""
		},
		"msgid": "<lorem-002@ipsum.example>",
		"list_archive_url": null,
		"date": "2026-03-05T20:47:07",
		"name": "[dev,v5] Consectetur adipiscing elit sed do",
		"commit_ref": null,
		"pull_url": null,
		"state": "new",
		"archived": false,
		"hash": "abc123",
		"submitter": {
			"id":82705, "url":"",
			"name":"Dolor Amet",
			"email":"dolor@amet.example"
		},
		"delegate": {
			"id": 77839,
			"url": "https://pw.example.com/api/1.2/users/77839/",
			"username": "sitamet",
			"first_name": "Sit",
			"last_name": "Amet",
			"email": "sit@amet.example"
		},
		"mbox": "",
		"series": [
			{
				"id":494616, "url":"",
				"web_url":"", "name":"",
				"date":"", "version":5,
				"mbox":""
			}
		],
		"comments": "",
		"check": "success",
		"checks": "",
		"tags": {},
		"related": []
	}`
	var p Patch
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Delegate == nil {
		t.Fatal("Delegate is nil")
	}
	if p.Delegate.Username != "sitamet" {
		t.Errorf("Delegate.Username = %q",
			p.Delegate.Username)
	}
	if p.Delegate.ID != 77839 {
		t.Errorf("Delegate.ID = %d", p.Delegate.ID)
	}
	if len(p.Series) != 1 || p.Series[0].ID != 494616 {
		t.Errorf("Series = %v", p.Series)
	}
}

func TestUnmarshalPatchDetail(t *testing.T) {
	raw := `{
		"id": 512232,
		"url": "https://pw.example.com/api/1.2/patches/512232/",
		"web_url": "",
		"project": {
			"id":47, "url":"",
			"name":"Example Project",
			"link_name":"example-project",
			"list_id":"", "list_email":"",
			"web_url":"", "scm_url":"",
			"webscm_url":"",
			"list_archive_url":"",
			"list_archive_url_format":"",
			"commit_url_format":""
		},
		"msgid": "<lorem-001@ipsum.example>",
		"list_archive_url": null,
		"date": "2015-08-30T14:40:31",
		"name": "[dev] Lorem ipsum dolor sit amet",
		"commit_ref": null,
		"pull_url": null,
		"state": "accepted",
		"archived": false,
		"hash": "25f025f83ba33cbef32475295bf3da2984aac746",
		"submitter": {
			"id":10471, "url":"",
			"name":"Lorem Ipsum",
			"email":"lorem@ipsum.example"
		},
		"delegate": null,
		"mbox": "",
		"series": [],
		"comments": "",
		"check": "pending",
		"checks": "",
		"tags": {},
		"related": [],
		"headers": {
			"Return-Path": "<dev-bounces@example.com>"
		},
		"content": "Lorem ipsum dolor sit amet, consectetur\nadipiscing elit sed do eiusmod.\n\nSigned-off-by: Lorem Ipsum <lorem@ipsum.example>",
		"diff": "--- a/lib/flow.h\n+++ b/lib/flow.h\n@@ -1,3 +1,3 @@\n-old\n+new",
		"prefixes": ["dev"]
	}`
	var pd PatchDetail
	if err := json.Unmarshal([]byte(raw), &pd); err != nil {
		t.Fatal(err)
	}
	if pd.ID != 512232 {
		t.Errorf("ID = %d", pd.ID)
	}
	if pd.Content == "" {
		t.Error("Content is empty")
	}
	if pd.Diff == "" {
		t.Error("Diff is empty")
	}
	if len(pd.Prefixes) != 1 || pd.Prefixes[0] != "dev" {
		t.Errorf("Prefixes = %v", pd.Prefixes)
	}
	if pd.Delegate != nil {
		t.Errorf("Delegate = %v, want nil", pd.Delegate)
	}
	if _, ok := pd.Headers["Return-Path"]; !ok {
		t.Error("Headers missing Return-Path")
	}
}

func TestUnmarshalSeries_RealDPDK(t *testing.T) {
	raw := `{
		"id": 13,
		"url": "https://pw.example.com/api/1.3/series/13/",
		"web_url": "https://pw.example.com/project/example-project/list/?series=13",
		"project": {
			"id":1, "url":"",
			"name":"Example Project",
			"link_name":"example-project",
			"list_id":"dev.example.com",
			"list_email":"dev@example.com",
			"web_url":"https://www.example.com",
			"scm_url":"git://git.example.com/example-project",
			"webscm_url":"https://git.example.com/example-project",
			"list_archive_url":"https://inbox.example.com/dev",
			"list_archive_url_format":"https://inbox.example.com/dev/{}",
			"commit_url_format":""
		},
		"name": "[dev] Tempor incididunt ut labore et dolore magna",
		"date": "2018-06-06T13:50:27",
		"submitter": {
			"id":685, "url":"",
			"name":"Consectetur Adip",
			"email":"consectetur@adip.example"
		},
		"version": 1,
		"total": 1,
		"received_total": 1,
		"received_all": true,
		"mbox": "https://pw.example.com/series/13/mbox/",
		"cover_letter": null,
		"patches": [
			{
				"id": 40680,
				"url": "https://pw.example.com/api/1.3/patches/40680/",
				"web_url": "https://pw.example.com/project/example-project/patch/20180606135027.13780-1-consectetur@adip.example/",
				"msgid": "<lorem-003@adip.example>",
				"list_archive_url": "https://inbox.example.com/dev/20180606135027.13780-1-consectetur@adip.example",
				"date": "2018-06-06T13:50:27",
				"name": "[dev] Tempor incididunt ut labore et dolore magna",
				"mbox": "https://pw.example.com/project/example-project/patch/20180606135027.13780-1-consectetur@adip.example/mbox/"
			}
		]
	}`
	var s Series
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if s.ID != 13 {
		t.Errorf("ID = %d", s.ID)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d", s.Version)
	}
	if s.Total != 1 {
		t.Errorf("Total = %d", s.Total)
	}
	if !s.ReceivedAll {
		t.Error("ReceivedAll = false")
	}
	if s.CoverLetter != nil {
		t.Errorf("CoverLetter = %v, want nil",
			s.CoverLetter)
	}
	if len(s.Patches) != 1 {
		t.Fatalf("len(Patches) = %d", len(s.Patches))
	}
	wantMsgID := "<lorem-003@adip.example>"
	if s.Patches[0].MsgID != wantMsgID {
		t.Errorf("Patches[0].MsgID = %q",
			s.Patches[0].MsgID)
	}
	if s.Submitter.Name != "Consectetur Adip" {
		t.Errorf("Submitter.Name = %q",
			s.Submitter.Name)
	}
}

func TestUnmarshalSeriesWithCover(t *testing.T) {
	raw := `{
		"id": 50,
		"url": "",
		"web_url": "",
		"project": ` + testProjectJSON + `,
		"name": "Multi-patch series",
		"date": "2026-03-10T12:00:00",
		"submitter": {
			"id":1, "url":"",
			"name":"Elit Sed",
			"email":"elit@sed.example"
		},
		"version": 2,
		"total": 3,
		"received_total": 3,
		"received_all": true,
		"mbox": "",
		"cover_letter": {
			"id":99, "url":"", "web_url":"",
			"msgid":"<cover@sed.example>",
			"list_archive_url":null,
			"date":"2026-03-10T12:00:00",
			"name":"[PATCH 0/3] Fix series",
			"mbox":""
		},
		"patches": [
			{
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"",
				"name":"[PATCH 1/3] First",
				"mbox":""
			},
			{
				"id":101, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"",
				"name":"[PATCH 2/3] Second",
				"mbox":""
			},
			{
				"id":102, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"",
				"name":"[PATCH 3/3] Third",
				"mbox":""
			}
		]
	}`
	var s Series
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if s.CoverLetter == nil {
		t.Fatal("CoverLetter is nil")
	}
	if s.CoverLetter.ID != 99 {
		t.Errorf("CoverLetter.ID = %d", s.CoverLetter.ID)
	}
	if len(s.Patches) != 3 {
		t.Errorf("len(Patches) = %d", len(s.Patches))
	}
}

func TestUnmarshalProject_RealOzlabs(t *testing.T) {
	raw := `{
		"id": 47,
		"url": "https://pw.example.com/api/1.2/projects/47/",
		"name": "Example Project",
		"link_name": "example-project",
		"list_id": "dev.example.com",
		"list_email": "dev@example.com",
		"web_url": "https://www.example.com/",
		"scm_url": "git@git.example.com:example-project/repo.git",
		"webscm_url": "https://git.example.com/example-project/repo",
		"maintainers": [
			{
				"id":22,
				"url":"https://pw.example.com/api/1.2/users/22/",
				"username":"dolor",
				"first_name":"Dolor",
				"last_name":"Sitamet",
				"email":"dolor@sitamet.example"
			},
			{
				"id":25,
				"url":"https://pw.example.com/api/1.2/users/25/",
				"username":"amet",
				"first_name":"Amet",
				"last_name":"Consect",
				"email":"amet@consect.example"
			},
			{
				"id":1,
				"url":"https://pw.example.com/api/1.2/users/1/",
				"username":"adipis",
				"first_name":"Adipis",
				"last_name":"Cing",
				"email":"adipis@cing.example"
			}
		],
		"subject_match": "",
		"list_archive_url": "",
		"list_archive_url_format": "",
		"commit_url_format": ""
	}`
	var p Project
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Example Project" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.LinkName != "example-project" {
		t.Errorf("LinkName = %q", p.LinkName)
	}
	if len(p.Maintainers) != 3 {
		t.Fatalf("len(Maintainers) = %d",
			len(p.Maintainers))
	}
	if p.Maintainers[0].Username != "dolor" {
		t.Errorf("Maintainers[0].Username = %q",
			p.Maintainers[0].Username)
	}
	if p.Maintainers[2].Username != "adipis" {
		t.Errorf("Maintainers[2].Username = %q",
			p.Maintainers[2].Username)
	}
}

func TestUnmarshalCheck_RealDPDK(t *testing.T) {
	raw := `{
		"id": 662860,
		"url": "https://pw.example.com/api/1.3/patches/149566/checks/662860/",
		"user": {
			"id": 4476,
			"url": "https://pw.example.com/api/1.3/users/4476/",
			"username": "tempor",
			"first_name": "Tempor",
			"last_name": "",
			"email": "tempor@incid.example"
		},
		"date": "2025-01-02T09:00:21.088694",
		"state": "success",
		"target_url": "https://pw.example.com/archives/test-report/2025-January/838984.html",
		"context": "checkpatch",
		"description": "coding style OK"
	}`
	var c Check
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != 662860 {
		t.Errorf("ID = %d", c.ID)
	}
	if c.State != "success" {
		t.Errorf("State = %q", c.State)
	}
	if c.Context != "checkpatch" {
		t.Errorf("Context = %q", c.Context)
	}
	if c.User.Username != "tempor" {
		t.Errorf("User.Username = %q", c.User.Username)
	}
	wantTarget := "https://pw.example.com/archives/test-report/2025-January/838984.html"
	if c.TargetURL == nil || *c.TargetURL != wantTarget {
		t.Errorf("TargetURL = %v", c.TargetURL)
	}
	if c.Description == nil || *c.Description != "coding style OK" {
		t.Errorf("Description = %v", c.Description)
	}
}

func TestUnmarshalCheck_NullOptionals(t *testing.T) {
	raw := `{
		"id": 501,
		"url": "",
		"user": {
			"id":1, "url":"",
			"username":"bot",
			"first_name":"", "last_name":"",
			"email":""
		},
		"date": "2026-03-10T13:00:00",
		"state": "pending",
		"target_url": null,
		"context": "ci/test",
		"description": null
	}`
	var c Check
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.TargetURL != nil {
		t.Errorf("TargetURL = %v, want nil",
			c.TargetURL)
	}
	if c.Description != nil {
		t.Errorf("Description = %v, want nil",
			c.Description)
	}
}

func TestUnmarshalComment_RealOzlabs(t *testing.T) {
	raw := `{
		"id": 1119335,
		"web_url": "https://pw.example.com/comment/1119335/",
		"msgid": "<lorem-004@ipsum.example>",
		"list_archive_url": null,
		"date": "2015-08-31T17:56:34",
		"subject": "Re: [dev] [PATCH] Lorem ipsum dolor sit amet",
		"submitter": {
			"id": 67136,
			"url": "https://pw.example.com/api/1.2/people/67136/",
			"name": "Eiusmod Tempor",
			"email": "eiusmod@tempor.example"
		},
		"content": "Acked-by: Eiusmod Tempor <eiusmod@tempor.example>\r\nTested-by: Eiusmod Tempor <eiusmod@tempor.example>",
		"headers": {"From": "Eiusmod Tempor <eiusmod@tempor.example>"}
	}`
	var c Comment
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != 1119335 {
		t.Errorf("ID = %d", c.ID)
	}
	wantSubj := "Re: [dev] [PATCH] Lorem ipsum dolor sit amet"
	if c.Subject != wantSubj {
		t.Errorf("Subject = %q", c.Subject)
	}
	if c.Submitter.Name != "Eiusmod Tempor" {
		t.Errorf("Submitter.Name = %q",
			c.Submitter.Name)
	}
	if c.Content == "" {
		t.Error("Content is empty")
	}
	if c.URL != "" {
		t.Errorf("URL = %q, want empty (v1.2 omits this field)",
			c.URL)
	}
}

func TestUnmarshalCover(t *testing.T) {
	raw := `{
		"id": 99,
		"url": "https://pw.example.com/api/1.3/covers/99/",
		"web_url": "https://pw.example.com/cover/99/",
		"project": ` + testProjectJSON + `,
		"msgid": "<cover@sed.example>",
		"list_archive_url": null,
		"date": "2026-03-10T12:00:00",
		"name": "[PATCH 0/3] Fix series",
		"submitter": {
			"id":1, "url":"",
			"name":"Elit Sed",
			"email":"elit@sed.example"
		},
		"mbox": "https://pw.example.com/cover/99/mbox/",
		"series": [
			{
				"id":50, "url":"", "web_url":"",
				"name":"Fix series",
				"date":"2026-03-10T12:00:00",
				"version":2, "mbox":""
			}
		],
		"comments": "https://pw.example.com/api/1.3/covers/99/comments/"
	}`
	var c Cover
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != 99 {
		t.Errorf("ID = %d", c.ID)
	}
	if c.MsgID != "<cover@sed.example>" {
		t.Errorf("MsgID = %q", c.MsgID)
	}
	wantComments := "https://pw.example.com/api/1.3/covers/99/comments/"
	if c.Comments != wantComments {
		t.Errorf("Comments = %q", c.Comments)
	}
}

func TestUnmarshalCoverDetail(t *testing.T) {
	raw := `{
		"id": 99,
		"url": "",
		"web_url": "",
		"project": ` + testProjectJSON + `,
		"msgid": "<cover@sed.example>",
		"list_archive_url": null,
		"date": "2026-03-10T12:00:00",
		"name": "[PATCH 0/3] Fix series",
		"submitter": {
			"id":1, "url":"",
			"name":"Elit Sed",
			"email":"elit@sed.example"
		},
		"mbox": "",
		"series": [],
		"comments": "",
		"headers": {"From": "Elit Sed <elit@sed.example>"},
		"content": "Lorem ipsum dolor sit amet.\n\nElit"
	}`
	var cd CoverDetail
	if err := json.Unmarshal([]byte(raw), &cd); err != nil {
		t.Fatal(err)
	}
	if cd.Content != "Lorem ipsum dolor sit amet.\n\nElit" {
		t.Errorf("Content = %q", cd.Content)
	}
}

func testEventPayload(
	t *testing.T,
	raw string,
	wantCategory string,
	check func(t *testing.T, e Event),
) {
	t.Helper()
	var e Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Category != wantCategory {
		t.Errorf("Category = %q, want %q",
			e.Category, wantCategory)
	}
	if e.Payload == nil {
		t.Fatal("Payload is nil")
	}
	check(t, e)
}

func TestEventPatchStateChanged_RealOzlabs(t *testing.T) {
	raw := `{
		"id": 5012075,
		"category": "patch-state-changed",
		"project": {
			"id": 27,
			"url": "https://pw.example.com/api/1.2/projects/27/",
			"name": "Example Project Dev",
			"link_name": "example-project",
			"list_id": "dev.example.com",
			"list_email": "dev@example.com",
			"web_url": "",
			"scm_url": "",
			"webscm_url": "",
			"list_archive_url": "",
			"list_archive_url_format": "",
			"commit_url_format": ""
		},
		"date": "2026-03-16T23:01:03.237099",
		"actor": {
			"id": 51847,
			"url": "https://pw.example.com/api/1.2/users/51847/",
			"username": "lorem",
			"first_name": "Lorem",
			"last_name": "Ipsum",
			"email": "lorem@ipsum.example"
		},
		"payload": {
			"patch": {
				"id": 1997083,
				"url": "https://pw.example.com/api/1.2/patches/1997083/",
				"web_url": "https://pw.example.com/project/example-project/patch/20241014-lorem-ipsum-v1-1-70a2fa042ba1@dolor.example/",
				"msgid": "<lorem-005@dolor.example>",
				"list_archive_url": null,
				"date": "2024-10-14T20:07:48",
				"name": "Sed ut perspiciatis unde omnis iste natus",
				"mbox": "https://pw.example.com/project/example-project/patch/20241014-lorem-ipsum-v1-1-70a2fa042ba1@dolor.example/mbox/"
			},
			"previous_state": "new",
			"current_state": "superseded"
		}
	}`
	testEventPayload(t, raw, "patch-state-changed",
		func(t *testing.T, e Event) {
			if e.Actor == nil {
				t.Fatal("Actor is nil")
			}
			if e.Actor.Username != "lorem" {
				t.Errorf("Actor.Username = %q",
					e.Actor.Username)
			}
			p, ok := e.Payload.(*PatchStateChangedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.PreviousState != "new" {
				t.Errorf("PreviousState = %q",
					p.PreviousState)
			}
			if p.CurrentState != "superseded" {
				t.Errorf("CurrentState = %q",
					p.CurrentState)
			}
			if p.Patch.ID != 1997083 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
		})
}

func TestEventPatchCreated(t *testing.T) {
	raw := `{
		"id": 1,
		"category": "patch-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"<patch@sed.example>",
				"list_archive_url":null,
				"date":"2026-03-10T12:00:00",
				"name":"[PATCH] Fix",
				"mbox":""
			}
		}
	}`
	testEventPayload(t, raw, "patch-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 100 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
		})
}

func TestEventCoverCreated(t *testing.T) {
	raw := `{
		"id": 2,
		"category": "cover-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"cover": {
				"id":99, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"",
				"name":"[PATCH 0/3] Series",
				"mbox":""
			}
		}
	}`
	testEventPayload(t, raw, "cover-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CoverCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Cover.ID != 99 {
				t.Errorf("Cover.ID = %d", p.Cover.ID)
			}
		})
}

func TestEventPatchDelegated(t *testing.T) {
	raw := `{
		"id": 4,
		"category": "patch-delegated",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": {
			"id":42, "url":"",
			"username":"admin",
			"first_name":"", "last_name":"",
			"email":""
		},
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"previous_delegate": null,
			"current_delegate": {
				"id":55, "url":"",
				"username":"reviewer",
				"first_name":"Magna",
				"last_name":"Aliqua",
				"email":"magna@aliqua.example"
			}
		}
	}`
	testEventPayload(t, raw, "patch-delegated",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchDelegatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.PreviousDelegate != nil {
				t.Errorf("PreviousDelegate = %v, want nil",
					p.PreviousDelegate)
			}
			if p.CurrentDelegate == nil ||
				p.CurrentDelegate.Username != "reviewer" {
				t.Errorf("CurrentDelegate = %v",
					p.CurrentDelegate)
			}
		})
}

func TestEventPatchCompleted(t *testing.T) {
	raw := `{
		"id": 5,
		"category": "patch-completed",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"series": {
				"id":50, "url":"", "web_url":"",
				"name":"Series", "date":"",
				"version":1, "mbox":""
			}
		}
	}`
	testEventPayload(t, raw, "patch-completed",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCompletedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 100 || p.Series.ID != 50 {
				t.Errorf("Patch.ID=%d Series.ID=%d",
					p.Patch.ID, p.Series.ID)
			}
		})
}

func TestEventCheckCreated(t *testing.T) {
	raw := `{
		"id": 6,
		"category": "check-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": {
			"id":1, "url":"",
			"username":"velit",
			"first_name":"", "last_name":"",
			"email":""
		},
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"check": {
				"id":500, "url":"",
				"date":"2026-03-10T13:00:00",
				"state":"success",
				"target_url":"https://ci.example.com/123",
				"context":"ci/build"
			}
		}
	}`
	testEventPayload(t, raw, "check-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CheckCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Check.State != "success" ||
				p.Check.Context != "ci/build" {
				t.Errorf("Check = %+v", p.Check)
			}
		})
}

func TestEventSeriesCreated(t *testing.T) {
	raw := `{
		"id": 7,
		"category": "series-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"series": {
				"id":50, "url":"", "web_url":"",
				"name":"New series",
				"date":"2026-03-10T12:00:00",
				"version":1, "mbox":""
			}
		}
	}`
	testEventPayload(t, raw, "series-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*SeriesCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Series.Name != "New series" {
				t.Errorf("Series.Name = %q",
					p.Series.Name)
			}
		})
}

func TestEventSeriesCompleted(t *testing.T) {
	raw := `{
		"id": 8,
		"category": "series-completed",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"series": {
				"id":50, "url":"", "web_url":"",
				"name":"Completed", "date":"",
				"version":1, "mbox":""
			}
		}
	}`
	testEventPayload(t, raw, "series-completed",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*SeriesCompletedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Series.Name != "Completed" {
				t.Errorf("Series.Name = %q",
					p.Series.Name)
			}
		})
}

func TestEventPatchCommentCreated(t *testing.T) {
	raw := `{
		"id": 9,
		"category": "patch-comment-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"comment": {
				"id":300, "url":"", "web_url":"",
				"msgid":"<reply@sed.example>",
				"list_archive_url":null,
				"date":"2026-03-11T09:00:00"
			}
		}
	}`
	testEventPayload(t, raw, "patch-comment-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCommentCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Comment.ID != 300 || p.Patch.ID != 100 {
				t.Errorf("Comment.ID=%d Patch.ID=%d",
					p.Comment.ID, p.Patch.ID)
			}
		})
}

func TestEventCoverCommentCreated(t *testing.T) {
	raw := `{
		"id": 10,
		"category": "cover-comment-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"cover": {
				"id":99, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"comment": {
				"id":301, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":""
			}
		}
	}`
	testEventPayload(t, raw, "cover-comment-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CoverCommentCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Cover.ID != 99 || p.Comment.ID != 301 {
				t.Errorf("Cover.ID=%d Comment.ID=%d",
					p.Cover.ID, p.Comment.ID)
			}
		})
}

func TestEventPatchRelationChanged(t *testing.T) {
	raw := `{
		"id": 11,
		"category": "patch-relation-changed",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			},
			"previous_relation": null,
			"current_relation": "related-group-1"
		}
	}`
	testEventPayload(t, raw, "patch-relation-changed",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchRelationChangedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.PreviousRelation != nil {
				t.Errorf("PreviousRelation = %v, want nil",
					p.PreviousRelation)
			}
			if p.CurrentRelation == nil ||
				*p.CurrentRelation != "related-group-1" {
				t.Errorf("CurrentRelation = %v",
					p.CurrentRelation)
			}
		})
}

func TestEventNullActor(t *testing.T) {
	raw := `{
		"id": 20,
		"category": "patch-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {
			"patch": {
				"id":100, "url":"", "web_url":"",
				"msgid":"",
				"list_archive_url":null,
				"date":"", "name":"", "mbox":""
			}
		}
	}`
	var e Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Actor != nil {
		t.Errorf("Actor = %v, want nil", e.Actor)
	}
}

func TestEventUnknownCategory(t *testing.T) {
	raw := `{
		"id": 30,
		"category": "some-future-event",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-10T12:00:00",
		"actor": null,
		"payload": {"something": "unknown"}
	}`
	var e Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Payload != nil {
		t.Errorf("Payload = %v, want nil for unknown",
			e.Payload)
	}
}

func TestPatchNullableFields(t *testing.T) {
	raw := `{
		"id": 100,
		"url": "",
		"web_url": "",
		"msgid": "",
		"list_archive_url": null,
		"date": "2026-03-10T12:00:00",
		"name": "test",
		"commit_ref": null,
		"pull_url": null,
		"state": "new",
		"archived": false,
		"hash": null,
		"submitter": {
			"id":1, "url":"",
			"name":"Elit Sed", "email":""
		},
		"delegate": null,
		"mbox": "",
		"series": [],
		"comments": "",
		"check": "pending",
		"checks": "",
		"tags": {},
		"related": [],
		"project": ` + testProjectJSON + `
	}`
	var p Patch
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.CommitRef != nil {
		t.Errorf("CommitRef = %v, want nil", p.CommitRef)
	}
	if p.PullURL != nil {
		t.Errorf("PullURL = %v, want nil", p.PullURL)
	}
	if p.Hash != nil {
		t.Errorf("Hash = %v, want nil", p.Hash)
	}
	if p.ListArchiveURL != nil {
		t.Errorf("ListArchiveURL = %v, want nil",
			p.ListArchiveURL)
	}
	if p.Delegate != nil {
		t.Errorf("Delegate = %v, want nil", p.Delegate)
	}
}

// Tests below model JSON on real event structures from Ozlabs (v1.2),
// DPDK (v1.3), and kernel.org (v1.3), with lorem ipsum data.

func TestEventCheckCreated_OzlabsStyle(t *testing.T) {
	raw := `{
		"id": 5015698,
		"category": "check-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:21:43.098134",
		"actor": {
			"id": 158449,
			"url": "http://pw.example.com/api/1.2/users/158449/",
			"username": "loremci",
			"first_name": "Lorem CI",
			"last_name": "",
			"email": "lorem-ci@ipsum.example"
		},
		"payload": {
			"patch": {
				"id": 2212684,
				"url": "http://pw.example.com/api/1.2/patches/2212684/",
				"web_url": "http://pw.example.com/project/lorem/patch/20260318-dolor-sit-amet@ipsum.example/",
				"msgid": "<20260318-dolor-sit-amet@ipsum.example>",
				"list_archive_url": null,
				"date": "2026-03-18T14:41:13",
				"name": "[RFC,1/1] Lorem ipsum dolor sit amet",
				"mbox": "http://pw.example.com/project/lorem/patch/20260318-dolor-sit-amet@ipsum.example/mbox/"
			},
			"check": {
				"id": 375235,
				"url": "http://pw.example.com/api/1.2/patches/2212684/checks/375235/",
				"date": "2026-03-18T15:21:43.094748",
				"state": "success",
				"target_url": "https://ci.example.com/actions/runs/23251777489",
				"context": "consectetur_adipiscing"
			}
		}
	}`
	testEventPayload(t, raw, "check-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CheckCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 2212684 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
			if p.Patch.ListArchiveURL != nil {
				t.Errorf("Patch.ListArchiveURL = %v, want nil", p.Patch.ListArchiveURL)
			}
			if p.Check.ID != 375235 {
				t.Errorf("Check.ID = %d", p.Check.ID)
			}
			if p.Check.State != "success" {
				t.Errorf("Check.State = %q", p.Check.State)
			}
			if p.Check.Context != "consectetur_adipiscing" {
				t.Errorf("Check.Context = %q", p.Check.Context)
			}
			if e.Actor == nil || e.Actor.Username != "loremci" {
				t.Errorf("Actor = %v", e.Actor)
			}
		})
}

func TestEventPatchCreated_OzlabsStyle(t *testing.T) {
	raw := `{
		"id": 5015694,
		"category": "patch-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:20:55.583773",
		"actor": null,
		"payload": {
			"patch": {
				"id": 2212729,
				"url": "http://pw.example.com/api/1.2/patches/2212729/",
				"web_url": "http://pw.example.com/project/lorem/patch/20260318-tempor-v1-1-consectetur@adip.example/",
				"msgid": "<20260318-tempor-v1-1-consectetur@adip.example>",
				"list_archive_url": null,
				"date": "2026-03-18T15:07:17",
				"name": "Tempor incididunt ut labore et dolore magna",
				"mbox": "http://pw.example.com/project/lorem/patch/20260318-tempor-v1-1-consectetur@adip.example/mbox/"
			}
		}
	}`
	testEventPayload(t, raw, "patch-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 2212729 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
			if p.Patch.ListArchiveURL != nil {
				t.Errorf("Patch.ListArchiveURL = %v, want nil", p.Patch.ListArchiveURL)
			}
			if e.Actor != nil {
				t.Errorf("Actor = %v, want nil", e.Actor)
			}
		})
}

func TestEventCoverCreated_OzlabsStyle(t *testing.T) {
	raw := `{
		"id": 5015684,
		"category": "cover-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:16:25.233650",
		"actor": null,
		"payload": {
			"cover": {
				"id": 2212725,
				"url": "http://pw.example.com/api/1.2/covers/2212725/",
				"web_url": "http://pw.example.com/project/lorem/cover/20260318-dolor-sit-amet@adip.example/",
				"msgid": "<20260318-dolor-sit-amet@adip.example>",
				"list_archive_url": null,
				"date": "2026-03-18T12:05:04",
				"name": "[lorem,0/3] Sed ut perspiciatis unde omnis iste natus",
				"mbox": "http://pw.example.com/project/lorem/cover/20260318-dolor-sit-amet@adip.example/mbox/"
			}
		}
	}`
	testEventPayload(t, raw, "cover-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CoverCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Cover.ID != 2212725 {
				t.Errorf("Cover.ID = %d", p.Cover.ID)
			}
			if p.Cover.ListArchiveURL != nil {
				t.Errorf("Cover.ListArchiveURL = %v, want nil", p.Cover.ListArchiveURL)
			}
			if p.Cover.Name != "[lorem,0/3] Sed ut perspiciatis unde omnis iste natus" {
				t.Errorf("Cover.Name = %q", p.Cover.Name)
			}
		})
}

func TestEventPatchDelegated_Redelegation(t *testing.T) {
	raw := `{
		"id": 1318924,
		"category": "patch-delegated",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T09:07:19.226607",
		"actor": {
			"id": 24651,
			"url": "https://pw.example.com/api/1.3/users/24651/",
			"username": "dolor",
			"first_name": "Dolor",
			"last_name": "Amet",
			"email": "dolor@amet.example"
		},
		"payload": {
			"patch": {
				"id": 162499,
				"url": "https://pw.example.com/api/1.3/patches/162499/",
				"web_url": "https://pw.example.com/project/lorem/patch/20260317-consectetur@adip.example/",
				"msgid": "<20260317-consectetur@adip.example>",
				"list_archive_url": "https://inbox.example.com/dev/20260317-consectetur@adip.example",
				"date": "2026-03-18T03:38:56",
				"name": "Lorem ipsum: skip test if vdev not available",
				"mbox": "https://pw.example.com/project/lorem/patch/20260317-consectetur@adip.example/mbox/"
			},
			"previous_delegate": {
				"id": 1,
				"url": "https://pw.example.com/api/1.3/users/1/",
				"username": "tempor",
				"first_name": "Tempor",
				"last_name": "Incididunt",
				"email": "tempor@incididunt.example"
			},
			"current_delegate": {
				"id": 24651,
				"url": "https://pw.example.com/api/1.3/users/24651/",
				"username": "dolor",
				"first_name": "Dolor",
				"last_name": "Amet",
				"email": "dolor@amet.example"
			}
		}
	}`
	testEventPayload(t, raw, "patch-delegated",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchDelegatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.PreviousDelegate == nil {
				t.Fatal("PreviousDelegate is nil")
			}
			if p.PreviousDelegate.Username != "tempor" {
				t.Errorf("PreviousDelegate.Username = %q", p.PreviousDelegate.Username)
			}
			if p.CurrentDelegate == nil {
				t.Fatal("CurrentDelegate is nil")
			}
			if p.CurrentDelegate.Username != "dolor" {
				t.Errorf("CurrentDelegate.Username = %q", p.CurrentDelegate.Username)
			}
			archiveURL := "https://inbox.example.com/dev/20260317-consectetur@adip.example"
			if p.Patch.ListArchiveURL == nil || *p.Patch.ListArchiveURL != archiveURL {
				t.Errorf("Patch.ListArchiveURL = %v", p.Patch.ListArchiveURL)
			}
		})
}

func TestEventPatchCommentCreated_V13(t *testing.T) {
	raw := `{
		"id": 1319053,
		"category": "patch-comment-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:17:25.797675",
		"actor": null,
		"payload": {
			"patch": {
				"id": 162206,
				"url": "https://pw.example.com/api/1.3/patches/162206/",
				"web_url": "https://pw.example.com/project/lorem/patch/20260306-dolor-v3-4-sit@amet.example/",
				"msgid": "<20260306-dolor-v3-4-sit@amet.example>",
				"list_archive_url": "https://inbox.example.com/dev/20260306-dolor-v3-4-sit@amet.example",
				"date": "2026-03-06T13:30:21",
				"name": "[v3,03/17] Consectetur adipiscing elit sed do eiusmod",
				"mbox": "https://pw.example.com/project/lorem/patch/20260306-dolor-v3-4-sit@amet.example/mbox/"
			},
			"comment": {
				"id": 183352,
				"url": "https://pw.example.com/api/1.3/patches/162206/comments/183352/",
				"web_url": "https://pw.example.com/comment/183352/",
				"msgid": "<LOREM+IPSUM+dolor@sit.amet.example>",
				"list_archive_url": "https://inbox.example.com/dev/LOREM+IPSUM+dolor@sit.amet.example",
				"date": "2026-03-18T15:17:10"
			}
		}
	}`
	testEventPayload(t, raw, "patch-comment-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCommentCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 162206 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
			if p.Comment.ID != 183352 {
				t.Errorf("Comment.ID = %d", p.Comment.ID)
			}
			archiveURL := "https://inbox.example.com/dev/LOREM+IPSUM+dolor@sit.amet.example"
			if p.Comment.ListArchiveURL == nil || *p.Comment.ListArchiveURL != archiveURL {
				t.Errorf("Comment.ListArchiveURL = %v", p.Comment.ListArchiveURL)
			}
			if e.Actor != nil {
				t.Errorf("Actor = %v, want nil", e.Actor)
			}
		})
}

func TestEventCoverCommentCreated_V13(t *testing.T) {
	raw := `{
		"id": 1318719,
		"category": "cover-comment-created",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-17T15:55:58.991314",
		"actor": null,
		"payload": {
			"cover": {
				"id": 3064,
				"url": "https://pw.example.com/api/1.3/covers/3064/",
				"web_url": "https://pw.example.com/project/lorem/cover/20260312-dolor-sit@amet.example/",
				"msgid": "<20260312-dolor-sit@amet.example>",
				"list_archive_url": "https://inbox.example.com/dev/20260312-dolor-sit@amet.example",
				"date": "2026-03-12T10:36:02",
				"name": "[0/4] Consectetur adipiscing fixes",
				"mbox": "https://pw.example.com/project/lorem/cover/20260312-dolor-sit@amet.example/mbox/"
			},
			"comment": {
				"id": 2353,
				"url": "https://pw.example.com/api/1.3/covers/3064/comments/2353/",
				"web_url": "https://pw.example.com/comment/2353/",
				"msgid": "<6225470.loremIpsum@tempor>",
				"list_archive_url": "https://inbox.example.com/dev/6225470.loremIpsum@tempor",
				"date": "2026-03-17T15:55:53"
			}
		}
	}`
	testEventPayload(t, raw, "cover-comment-created",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*CoverCommentCreatedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Cover.ID != 3064 {
				t.Errorf("Cover.ID = %d", p.Cover.ID)
			}
			if p.Comment.ID != 2353 {
				t.Errorf("Comment.ID = %d", p.Comment.ID)
			}
			coverArchive := "https://inbox.example.com/dev/20260312-dolor-sit@amet.example"
			if p.Cover.ListArchiveURL == nil || *p.Cover.ListArchiveURL != coverArchive {
				t.Errorf("Cover.ListArchiveURL = %v", p.Cover.ListArchiveURL)
			}
			commentArchive := "https://inbox.example.com/dev/6225470.loremIpsum@tempor"
			if p.Comment.ListArchiveURL == nil || *p.Comment.ListArchiveURL != commentArchive {
				t.Errorf("Comment.ListArchiveURL = %v", p.Comment.ListArchiveURL)
			}
		})
}

func TestEventPatchCompleted_KernelStyle(t *testing.T) {
	raw := `{
		"id": 21265516,
		"category": "patch-completed",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:17:31.616206",
		"actor": null,
		"payload": {
			"patch": {
				"id": 14481849,
				"url": "https://pw.example.com/api/1.3/patches/14481849/",
				"web_url": "https://pw.example.com/project/lorem/patch/20260318-topic-dolor-v3-7-consectetur@adip.example/",
				"msgid": "<20260318-topic-dolor-v3-7-consectetur@adip.example>",
				"list_archive_url": "https://lore.example.com/r/20260318-topic-dolor-v3-7-consectetur@adip.example",
				"date": "2026-03-18T15:13:13",
				"name": "[v3,7/7] Sed ut perspiciatis unde omnis iste natus error",
				"mbox": "https://pw.example.com/project/lorem/patch/20260318-topic-dolor-v3-7-consectetur@adip.example/mbox/"
			},
			"series": {
				"id": 1068714,
				"url": "https://pw.example.com/api/1.3/series/1068714/",
				"web_url": "https://pw.example.com/project/lorem/list/?series=1068714",
				"date": "2026-03-18T15:13:06",
				"name": "Voluptatem accusantium doloremque laudantium",
				"version": 3,
				"mbox": "https://pw.example.com/series/1068714/mbox/"
			}
		}
	}`
	testEventPayload(t, raw, "patch-completed",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*PatchCompletedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Patch.ID != 14481849 {
				t.Errorf("Patch.ID = %d", p.Patch.ID)
			}
			if p.Series.ID != 1068714 {
				t.Errorf("Series.ID = %d", p.Series.ID)
			}
			if p.Series.Version != 3 {
				t.Errorf("Series.Version = %d", p.Series.Version)
			}
			archiveURL := "https://lore.example.com/r/20260318-topic-dolor-v3-7-consectetur@adip.example"
			if p.Patch.ListArchiveURL == nil || *p.Patch.ListArchiveURL != archiveURL {
				t.Errorf("Patch.ListArchiveURL = %v", p.Patch.ListArchiveURL)
			}
			if e.Actor != nil {
				t.Errorf("Actor = %v, want nil", e.Actor)
			}
		})
}

func TestEventSeriesCompleted_KernelStyle(t *testing.T) {
	raw := `{
		"id": 21265517,
		"category": "series-completed",
		"project": ` + testProjectJSON + `,
		"date": "2026-03-18T15:17:31.629912",
		"actor": null,
		"payload": {
			"series": {
				"id": 1068714,
				"url": "https://pw.example.com/api/1.3/series/1068714/",
				"web_url": "https://pw.example.com/project/lorem/list/?series=1068714",
				"date": "2026-03-18T15:13:06",
				"name": "Voluptatem accusantium doloremque laudantium",
				"version": 3,
				"mbox": "https://pw.example.com/series/1068714/mbox/"
			}
		}
	}`
	testEventPayload(t, raw, "series-completed",
		func(t *testing.T, e Event) {
			p, ok := e.Payload.(*SeriesCompletedPayload)
			if !ok {
				t.Fatalf("Payload type = %T", e.Payload)
			}
			if p.Series.ID != 1068714 {
				t.Errorf("Series.ID = %d", p.Series.ID)
			}
			if p.Series.Version != 3 {
				t.Errorf("Series.Version = %d", p.Series.Version)
			}
			if p.Series.Name != "Voluptatem accusantium doloremque laudantium" {
				t.Errorf("Series.Name = %q", p.Series.Name)
			}
		})
}
