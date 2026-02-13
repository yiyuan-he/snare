package diff

import (
	"testing"
)

func TestParse_GoFilesOnly(t *testing.T) {
	raw := `diff --git a/main.go b/main.go
index 1234567..abcdefg 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+// added comment
 func main() {}
diff --git a/readme.md b/readme.md
index 1234567..abcdefg 100644
--- a/readme.md
+++ b/readme.md
@@ -1 +1,2 @@
 # Title
+Added line
`

	e := &Extractor{Dir: "/fake/dir"}
	result, err := e.parse(raw)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1 (only .go files)", len(result))
	}

	if result[0].OldName != "main.go" {
		t.Errorf("OldName = %q, want %q", result[0].OldName, "main.go")
	}
}

func TestParse_ExcludesTestFiles(t *testing.T) {
	raw := `diff --git a/foo_test.go b/foo_test.go
index 1234567..abcdefg 100644
--- a/foo_test.go
+++ b/foo_test.go
@@ -1,3 +1,4 @@
 package foo

+// new test
 func TestFoo() {}
`

	e := &Extractor{Dir: "/fake/dir"}
	result, err := e.parse(raw)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0 (test files excluded)", len(result))
	}
}

func TestParse_HunkContent(t *testing.T) {
	raw := `diff --git a/pkg/util.go b/pkg/util.go
index 1234567..abcdefg 100644
--- a/pkg/util.go
+++ b/pkg/util.go
@@ -5,3 +5,4 @@
 func helper() {
-	old line
+	new line
+	extra line
 }
`

	e := &Extractor{Dir: "/fake/dir"}
	result, err := e.parse(raw)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	fd := result[0]
	if len(fd.Hunks) != 1 {
		t.Fatalf("len(hunks) = %d, want 1", len(fd.Hunks))
	}

	hunk := fd.Hunks[0]
	if hunk.NewStartLine != 5 {
		t.Errorf("NewStartLine = %d, want 5", hunk.NewStartLine)
	}

	// Content should contain the diff lines with +/- prefixes
	if hunk.Content == "" {
		t.Error("hunk content is empty")
	}
}
