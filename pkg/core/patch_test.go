package core

import (
	"reflect"
	"strings"
	"testing"
)

func patchStrPtr(value string) *string {
	return &value
}

func TestPatchParseHunks(t *testing.T) {
	hunks, err := PatchParse(
		"*** Begin Patch\n*** Add File: add.txt\n+added\n*** Update File: update.txt\n@@ section\n-old\n+new\n*** Delete File: delete.txt\n*** End Patch",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []PatchHunk{
		PatchAddHunk{Path: "add.txt", Contents: "added"},
		PatchUpdateHunk{
			Path: "update.txt",
			Chunks: []PatchUpdateFileChunk{{
				OldLines:      []string{"old"},
				NewLines:      []string{"new"},
				ChangeContext: patchStrPtr("section"),
			}},
		},
		PatchDeleteHunk{Path: "delete.txt"},
	}

	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("unexpected hunks:\n got %#v\nwant %#v", hunks, want)
	}
}

func TestPatchParseStripsHeredocWrapper(t *testing.T) {
	hunks, err := PatchParse("cat <<'EOF'\n*** Begin Patch\n*** Add File: add.txt\n+added\n*** End Patch\nEOF")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []PatchHunk{PatchAddHunk{Path: "add.txt", Contents: "added"}}
	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("unexpected hunks: %#v", hunks)
	}
}

func TestPatchDeriveFuzzyLineUpdatePreservesBom(t *testing.T) {
	update, err := PatchDerive(
		"update.txt",
		[]PatchUpdateFileChunk{{OldLines: []string{"  old   "}, NewLines: []string{"new"}}},
		"\ufeffold\n",
	)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if update.Content != "new\n" || !update.Bom {
		t.Fatalf("unexpected update: %#v", update)
	}
	if got := PatchJoinBom(update.Content, update.Bom); got != "\ufeffnew\n" {
		t.Fatalf("unexpected joinBom: %q", got)
	}
}

func TestPatchDeriveMatchesEofAnchoredChunk(t *testing.T) {
	update, err := PatchDerive(
		"update.txt",
		[]PatchUpdateFileChunk{{
			OldLines:  []string{"marker", "end"},
			NewLines:  []string{"marker changed", "end"},
			EndOfFile: true,
		}},
		"marker\nmiddle\nmarker\nend\n",
	)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if update.Content != "marker\nmiddle\nmarker changed\nend\n" {
		t.Fatalf("unexpected content: %q", update.Content)
	}
}

func TestPatchParseEofMarkerInsideUpdateChunk(t *testing.T) {
	hunks, err := PatchParse(
		"*** Begin Patch\n*** Update File: update.txt\n@@\n-last\n+end\n*** End of File\n*** End Patch",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []PatchHunk{
		PatchUpdateHunk{
			Path: "update.txt",
			Chunks: []PatchUpdateFileChunk{{
				OldLines:  []string{"last"},
				NewLines:  []string{"end"},
				EndOfFile: true,
			}},
		},
	}
	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("unexpected hunks: %#v", hunks)
	}
}

func TestPatchDeriveUsesChangeContext(t *testing.T) {
	update, err := PatchDerive(
		"f.txt",
		[]PatchUpdateFileChunk{{
			ChangeContext: patchStrPtr("section2"),
			OldLines:      []string{"old"},
			NewLines:      []string{"new"},
		}},
		"old\nsection1\nold\nsection2\nold\n",
	)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if want := "old\nsection1\nold\nsection2\nnew\n"; update.Content != want {
		t.Fatalf("got %q, want %q", update.Content, want)
	}
}

func TestPatchDeriveFailsWhenExpectedLinesMissing(t *testing.T) {
	_, err := PatchDerive(
		"f.txt",
		[]PatchUpdateFileChunk{{OldLines: []string{"missing"}, NewLines: []string{"x"}}},
		"a\nb\n",
	)
	if err == nil || !strings.Contains(err.Error(), "Failed to find expected lines in f.txt") {
		t.Fatalf("expected missing-lines error, got %v", err)
	}
}

func TestPatchJoinBomRemovesAtMostOneBom(t *testing.T) {
	if got := PatchJoinBom("\ufeff\ufefftext", false); got != "\ufefftext" {
		t.Fatalf("expected a single leading BOM to remain, got %q", got)
	}
	if got := PatchJoinBom("text", true); got != "\ufefftext" {
		t.Fatalf("expected BOM to be added, got %q", got)
	}
}

func TestPatchParseTrimsLeadingBom(t *testing.T) {
	hunks, err := PatchParse("\ufeff*** Begin Patch\n*** Add File: a.txt\n+x\n*** End Patch")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []PatchHunk{PatchAddHunk{Path: "a.txt", Contents: "x"}}
	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("unexpected hunks: %#v", hunks)
	}
}

func TestPatchParseRejectsMalformedHunks(t *testing.T) {
	_, err := PatchParse("*** Begin Patch\n*** Add File: add.txt\nmissing plus\n*** End Patch")
	if err == nil || !strings.Contains(err.Error(), "Invalid add file line") {
		t.Fatalf("expected add file line error, got %v", err)
	}

	_, err = PatchParse("*** Begin Patch\n*** Update File: update.txt\n*** End Patch")
	if err == nil || !strings.Contains(err.Error(), "expected at least one @@ chunk") {
		t.Fatalf("expected update chunk error, got %v", err)
	}

	_, err = PatchParse("*** Begin Patch\n*** Delete File: delete.txt\nunexpected body\n*** End Patch")
	if err == nil || !strings.Contains(err.Error(), "Invalid patch line") {
		t.Fatalf("expected invalid patch line error, got %v", err)
	}
}
