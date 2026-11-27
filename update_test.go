package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStagedName(t *testing.T) {
	for in, want := range map[string]string{`C:\a\fragma.exe`: `C:\a\fragma.new.exe`, "/usr/bin/fragma": "/usr/bin/fragma.new", ".exe": ".exe.new"} {
		if got := stagedName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestProgramSwap(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "download.exe"), filepath.Join(dir, "fragma.exe")
	put := func(p, s string) {
		if err := writeFileAtomic(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	put(src, "new")
	put(dst, "old")
	ok := func(string) error { return nil }

	if _, err := stageProgram(src, dst, func(string) error { return errors.New("does not run") }); err == nil {
		t.Fatal("a program that does not run must be refused")
	}
	if read(t, dst) != "old" || fileExists(stagedName(dst)) {
		t.Fatal("refusing must change nothing")
	}

	staged, err := stageProgram(src, dst, ok)
	if err != nil {
		t.Fatal(err)
	}
	s, err := swapProgram(staged, dst)
	if err != nil || read(t, dst) != "new" {
		t.Fatal(err)
	}
	if err := s.undo(); err != nil || read(t, dst) != "old" {
		t.Fatal("undo must bring the old program back", err)
	}

	put(dst+".old", "older")
	staged, _ = stageProgram(src, dst, ok)
	s, _ = swapProgram(staged, dst)
	if err := s.keep(); err != nil || read(t, dst) != "new" || read(t, dst+".old") != "old" || fileExists(dst+".rollback") {
		t.Fatal("keep must leave the new program and the one before", err)
	}

	// a first install
	first := filepath.Join(dir, "first.exe")
	staged, _ = stageProgram(src, first, ok)
	s, err = swapProgram(staged, first)
	if err != nil || read(t, first) != "new" {
		t.Fatal(err)
	}
	if err := s.undo(); err != nil || fileExists(first) {
		t.Fatal("undoing a first install must leave nothing", err)
	}

	// the staged file vanished (an antivirus, say): the old program stays
	if _, err := swapProgram(filepath.Join(dir, "gone"), dst); err == nil || read(t, dst) != "new" {
		t.Fatal("a failed swap must keep the installed program", err)
	}
}
