package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestObjectKeyTemplate(t *testing.T) {
	at := time.Date(2027, 1, 1, 0, 5, 9, 0, time.FixedZone("UTC+8", 8*60*60))
	md5Hash := "0123456789abcdef0123456789abcdef"
	shaHash := md5Hash + md5Hash
	for _, test := range []struct{ template, want string }{
		{"{year}/{month}-{day}-{md5}.{extName}", "2027/01-01-" + md5Hash + ".png"},
		{"images/{sha256}.{extName}", "images/" + shaHash + ".png"},
		{"/{date}/{time}/{md5}.{extName}/", "2027-01-01/00-05-09/" + md5Hash + ".png"},
		{"{hour}/{minute}/{second}/{timestamp}.png", "00/05/09/1798733109.png"},
		{"{year}-{year}/{month}.png", "2027-2027/01.png"},
		{"images/{YEAR}/{unknown}/2006.png", "images/{YEAR}/{unknown}/2006.png"},
		{"/图 库/{date}/a.png/", "图 库/2027-01-01/a.png"},
		{"fixed.png", "fixed.png"},
	} {
		t.Run(test.template, func(t *testing.T) {
			c := Default()
			c.PathTemplate = test.template
			got, err := c.ObjectKey(at, md5Hash, shaHash, "png")
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
			if c.PathTemplate != test.template {
				t.Fatal("expansion mutated template")
			}
		})
	}
	c := Default()
	c.PathTemplate = "{date}/{md5}.{extName}"
	local, _ := c.ObjectKey(at, md5Hash, shaHash, "png")
	utc, _ := c.ObjectKey(at.UTC(), md5Hash, shaHash, "png")
	if local == utc {
		t.Fatal("date ignored the provided zone")
	}
}

func TestLegacyPrefixMigration(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{`{}`, Default().PathTemplate},
		{`{"prefix":"images"}`, "images/{sha256}.{extName}"},
		{`{"prefix":""}`, "{sha256}.{extName}"},
		{`{"prefix":"/images/{year}/{month}/{day}/"}`, "images/{year}/{month}/{day}/{sha256}.{extName}"},
		{`{"prefix":"old","path_template":"{md5}.{extName}"}`, "{md5}.{extName}"},
	} {
		t.Run(test.raw, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(test.raw), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(p)
			if err != nil || c.PathTemplate != test.want {
				t.Fatalf("migration: %q %v", c.PathTemplate, err)
			}
			raw, _ := os.ReadFile(p)
			if string(raw) != test.raw {
				t.Fatal("load rewrote configuration")
			}
			if err := Save(p, c); err != nil {
				t.Fatal(err)
			}
			saved, _ := os.ReadFile(p)
			if strings.Contains(string(saved), `"prefix"`) || !strings.Contains(string(saved), `"path_template"`) {
				t.Fatal("legacy field persisted", string(saved))
			}
			loaded, err := Load(p)
			if err != nil || !reflect.DeepEqual(loaded, c) {
				t.Fatalf("roundtrip: %+v %v", loaded, err)
			}
		})
	}
}

func TestTemplateEnvironmentAndLegacyAlias(t *testing.T) {
	c := Default()
	if err := c.Set("prefix", "images/{date}"); err != nil || c.PathTemplate != "images/{date}/{sha256}.{extName}" {
		t.Fatal(c.PathTemplate, err)
	}
	if err := c.Set("path-template", "{year}/{md5}.{extName}"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_PREFIX", "legacy/{year}")
	if err := c.ApplyEnv(); err != nil || c.PathTemplate != "legacy/{year}/{sha256}.{extName}" {
		t.Fatal(c.PathTemplate, err)
	}
	t.Setenv("PICUP_PATH_TEMPLATE", "{date}/{md5}.{extName}")
	if err := c.ApplyEnv(); err != nil || c.PathTemplate != "{date}/{md5}.{extName}" {
		t.Fatal(c.PathTemplate, err)
	}
}

func TestInvalidObjectPaths(t *testing.T) {
	for _, template := range []string{"", "///", "../{md5}.png", "images/./a.png", "images/../a.png", "a\\b.png", "a\nb.png", strings.Repeat("a", 1025)} {
		c := Default()
		c.PathTemplate = template
		if c.ValidateOptions() == nil {
			t.Fatalf("accepted %q", template)
		}
	}
	c := Default()
	c.PathTemplate = strings.Repeat("{sha256}", 20) + ".png"
	if _, err := c.ObjectKey(time.Now(), "md5", strings.Repeat("a", 64), "png"); err == nil {
		t.Fatal("accepted expanded key over 1024 bytes")
	}
}
