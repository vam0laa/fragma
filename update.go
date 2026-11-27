package main

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// buildFault is empty in every release. Tests set it at build time
// (-ldflags "-X main.buildFault=yes") to get a version that does not start.
var buildFault string

// programSwap puts a new program in the place of an installed one in a way that
// can be undone. It renames files only, which works on running programs too.
type programSwap struct {
	dst    string
	hadOld bool
}

// stagedName keeps the ending, so that Windows can run the file.
func stagedName(dst string) string {
	base, ext := dst, ""
	if i := strings.LastIndex(dst, ".exe"); i == len(dst)-4 && i > 0 {
		base, ext = dst[:i], ".exe"
	}
	return base + ".new" + ext
}

// stageProgram copies src next to dst and lets check try it. Nothing that is
// installed changes.
func stageProgram(src, dst string, check func(path string) error) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	staged := stagedName(dst)
	if err := writeFileAtomic(staged, b, 0o755); err != nil {
		return "", err
	}
	if err := check(staged); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

// swapProgram moves the installed program aside and the staged one into its place.
func swapProgram(staged, dst string) (programSwap, error) {
	s := programSwap{dst: dst}
	os.Remove(dst + ".rollback")
	err := os.Rename(dst, dst+".rollback")
	switch {
	case err == nil:
		s.hadOld = true
	case !errors.Is(err, fs.ErrNotExist):
		return s, err
	}
	if err := os.Rename(staged, dst); err != nil {
		return s, errors.Join(err, s.undo())
	}
	return s, nil
}

// undo puts the earlier program back, or removes the new one if there was none.
func (s programSwap) undo() error {
	// a program that still runs cannot be deleted, but it can be moved
	failed := s.dst + ".failed"
	os.Remove(failed)
	if err := os.Rename(s.dst, failed); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	os.Remove(failed)
	if !s.hadOld {
		return nil
	}
	return os.Rename(s.dst+".rollback", s.dst)
}

// keep makes the earlier program the kept old version.
func (s programSwap) keep() error {
	if !s.hadOld {
		return nil
	}
	os.Remove(s.dst + ".old")
	return os.Rename(s.dst+".rollback", s.dst+".old")
}
