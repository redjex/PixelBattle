package trophyrewards

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type Kind string

const (
	KindCode    Kind = "code"
	KindURL     Kind = "url"
	maxFileSize      = 1 << 20
)

var rewardKinds = map[string]Kind{
	"yng-explrz":            KindCode,
	"stashvpn":              KindCode,
	"besigned":              KindURL,
	"stickers":              KindURL,
	"stikidbot":             KindURL,
	"bear":                  KindURL,
	"bear-redjex":           KindURL,
	"liberty-figure-252202": KindURL,
	"candy-cane-162605":     KindURL,
	"vice-cream-227533":     KindURL,
	"vice-cream-428029":     KindURL,
	"chill-flame-303522":    KindURL,
}

type Catalog struct {
	directory string
}

func New(directory string) *Catalog {
	return &Catalog{directory: directory}
}

func (c *Catalog) Kind(trophyID string) (Kind, bool) {
	kind, ok := rewardKinds[trophyID]
	return kind, ok
}

func (c *Catalog) Entries(trophyID string) ([]string, error) {
	kind, ok := c.Kind(trophyID)
	if !ok {
		return nil, fmt.Errorf("unsupported trophy %q", trophyID)
	}
	path := filepath.Join(c.directory, trophyID+".txt")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return nil, fmt.Errorf("invalid reward file for %s", trophyID)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]string, 0)
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		value := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		if len(value) > 2048 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid reward value in %s", trophyID)
		}
		if kind == KindURL {
			parsed, parseErr := url.ParseRequestURI(value)
			if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
				return nil, fmt.Errorf("invalid https URL in %s", trophyID)
			}
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		entries = append(entries, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no rewards configured for %s", trophyID)
	}
	return entries, nil
}
