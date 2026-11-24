package main

import (
	"regexp"
	"strings"
	"testing"
)

var verbRE = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func verbCount(s string) int {
	return len(verbRE.FindAllString(strings.ReplaceAll(s, "%%", ""), -1))
}

func TestCatalogsMatch(t *testing.T) {
	for k, en := range catalog["en"] {
		tr, ok := catalog["tr"][k]
		if !ok {
			t.Errorf("TR is missing %q", k)
			continue
		}
		if verbCount(en) != verbCount(tr) {
			t.Errorf("%q: format verbs differ (EN %d, TR %d)", k, verbCount(en), verbCount(tr))
		}
	}
	for k := range catalog["tr"] {
		if _, ok := catalog["en"][k]; !ok {
			t.Errorf("EN is missing %q", k)
		}
	}
}

func TestDetectLang(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANG": "tr_TR.UTF-8"}, "tr"},
		{map[string]string{"LANG": "en_US.UTF-8"}, "en"},
		{map[string]string{"LANG": "de_DE.UTF-8"}, "en"},
		{map[string]string{"LANG": "en_US.UTF-8", "FRAGMA_LANG": "tr"}, "tr"},
		{map[string]string{"LANG": "tr_TR.UTF-8", "FRAGMA_LANG": "en"}, "en"},
		{map[string]string{"LC_ALL": "tr_TR.UTF-8", "LANG": "en_US.UTF-8"}, "tr"},
		{map[string]string{}, "en"},
	}
	for _, c := range cases {
		for _, k := range []string{"FRAGMA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
			t.Setenv(k, "")
		}
		for k, v := range c.env {
			t.Setenv(k, v)
		}
		if got := detectLang(); got != c.want {
			t.Errorf("%v -> %q, want %q", c.env, got, c.want)
		}
	}
}

func TestTFallback(t *testing.T) {
	old := lang
	defer func() { lang = old }()
	lang = "tr"
	if T("word.on") != "AÇIK" || T("no.such.key") != "no.such.key" {
		t.Error("TR lookup or fallback broken")
	}
	lang = "en"
	if T("word.on") != "ON" {
		t.Error("EN lookup broken")
	}
}
