// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"os/exec"
	"testing"
)

func skipIfNoDiff(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("diff binary not available")
	}
}

func TestRunCompareDiff_Identical(t *testing.T) {
	skipIfNoDiff(t)
	lines := []string{"lorem ipsum", "dolor sit amet", "consectetur"}
	got := runCompareDiff(lines, lines)
	want := "UUU"
	if got != want {
		t.Errorf("runCompareDiff identical = %q, want %q", got, want)
	}
}

func TestRunCompareDiff_AllDifferent(t *testing.T) {
	skipIfNoDiff(t)
	left := []string{"lorem", "ipsum"}
	right := []string{"dolor", "amet"}
	got := runCompareDiff(left, right)
	// diff outputs all old lines first, then all new lines
	want := "OONN"
	if got != want {
		t.Errorf("runCompareDiff all different = %q, want %q", got, want)
	}
}

func TestRunCompareDiff_Mixed(t *testing.T) {
	skipIfNoDiff(t)
	left := []string{"lorem", "ipsum", "dolor"}
	right := []string{"lorem", "amet", "dolor"}
	got := runCompareDiff(left, right)
	// "lorem" unchanged, "ipsum" old, "amet" new, "dolor" unchanged
	want := "UONU"
	if got != want {
		t.Errorf("runCompareDiff mixed = %q, want %q", got, want)
	}
}

func TestRunCompareDiff_LeftEmpty(t *testing.T) {
	skipIfNoDiff(t)
	right := []string{"lorem", "ipsum"}
	got := runCompareDiff(nil, right)
	if got != "NN" {
		t.Errorf("runCompareDiff left empty = %q, want %q", got, "NN")
	}
}

func TestRunCompareDiff_RightEmpty(t *testing.T) {
	skipIfNoDiff(t)
	left := []string{"lorem", "ipsum"}
	got := runCompareDiff(left, nil)
	if got != "OO" {
		t.Errorf("runCompareDiff right empty = %q, want %q", got, "OO")
	}
}

func TestRunCompareDiff_BothEmpty(t *testing.T) {
	skipIfNoDiff(t)
	got := runCompareDiff(nil, nil)
	if got != "" {
		t.Errorf("runCompareDiff both empty = %q, want %q", got, "")
	}
}

func TestAlignDiffLines_Unchanged(t *testing.T) {
	left := []string{"a", "b", "c"}
	right := []string{"a", "b", "c"}
	aL, aR, kL, kR := alignDiffLines(left, right, "UUU")
	if len(aL) != 3 || len(aR) != 3 {
		t.Fatalf("aligned lengths = %d, %d, want 3, 3", len(aL), len(aR))
	}
	for i := 0; i < 3; i++ {
		if kL[i] != diffUnchanged || kR[i] != diffUnchanged {
			t.Errorf("line %d: kinds = %d, %d, want unchanged", i, kL[i], kR[i])
		}
	}
}

func TestAlignDiffLines_Replacement(t *testing.T) {
	left := []string{"a", "b", "c"}
	right := []string{"a", "x", "c"}
	// "a" unchanged, "b" old + "x" new zipped on same row, "c" unchanged
	aL, aR, kL, kR := alignDiffLines(left, right, "UONU")
	if len(aL) != len(aR) {
		t.Fatalf("aligned lengths differ: %d vs %d", len(aL), len(aR))
	}
	if len(aL) != 3 {
		t.Fatalf("aligned length = %d, want 3", len(aL))
	}
	// Line 0: both "a", unchanged
	if aL[0] != "a" || aR[0] != "a" || kL[0] != diffUnchanged {
		t.Errorf("line 0: L=%q R=%q kL=%d", aL[0], aR[0], kL[0])
	}
	// Line 1: left "b" removed, right "x" added — same row
	if aL[1] != "b" || aR[1] != "x" || kL[1] != diffRemoved || kR[1] != diffAdded {
		t.Errorf("line 1: L=%q R=%q kL=%d kR=%d", aL[1], aR[1], kL[1], kR[1])
	}
	// Line 2: both "c", unchanged
	if aL[2] != "c" || aR[2] != "c" || kL[2] != diffUnchanged {
		t.Errorf("line 2: L=%q R=%q kL=%d", aL[2], aR[2], kL[2])
	}
}

func TestAlignDiffLines_UnequalBlock(t *testing.T) {
	left := []string{"a", "b1", "b2", "c"}
	right := []string{"a", "x", "c"}
	// "a" unchanged, "b1"+"b2" old zipped with "x" new, "c" unchanged
	aL, aR, kL, kR := alignDiffLines(left, right, "UOONU")
	if len(aL) != len(aR) {
		t.Fatalf("aligned lengths differ: %d vs %d", len(aL), len(aR))
	}
	if len(aL) != 4 {
		t.Fatalf("aligned length = %d, want 4", len(aL))
	}
	// Line 0: unchanged
	if aL[0] != "a" || aR[0] != "a" || kL[0] != diffUnchanged {
		t.Errorf("line 0: L=%q R=%q kL=%d", aL[0], aR[0], kL[0])
	}
	// Line 1: "b1" removed paired with "x" added
	if aL[1] != "b1" || aR[1] != "x" || kL[1] != diffRemoved || kR[1] != diffAdded {
		t.Errorf("line 1: L=%q R=%q kL=%d kR=%d", aL[1], aR[1], kL[1], kR[1])
	}
	// Line 2: "b2" removed, no pair — right gets padding
	if aL[2] != "b2" || aR[2] != "" || kL[2] != diffRemoved || kR[2] != diffPadding {
		t.Errorf("line 2: L=%q R=%q kL=%d kR=%d", aL[2], aR[2], kL[2], kR[2])
	}
	// Line 3: unchanged
	if aL[3] != "c" || aR[3] != "c" || kL[3] != diffUnchanged {
		t.Errorf("line 3: L=%q R=%q kL=%d", aL[3], aR[3], kL[3])
	}
}

func TestAlignDiffLines_EmptyClasses(t *testing.T) {
	left := []string{"a", "b"}
	right := []string{"a", "b", "c"}
	aL, aR, kL, kR := alignDiffLines(left, right, "")
	// Empty classes = no diff result, should pad to equal length.
	if len(aL) != len(aR) {
		t.Fatalf("should pad to equal: %d vs %d", len(aL), len(aR))
	}
	for i := range kL {
		if kL[i] != diffUnchanged || kR[i] != diffUnchanged {
			t.Errorf("line %d: should be unchanged, got %d/%d", i, kL[i], kR[i])
		}
	}
}
