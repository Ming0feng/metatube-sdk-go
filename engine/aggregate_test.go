package engine

import (
	"testing"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"

	"github.com/metatube-community/metatube-sdk-go/model"
)

func sampleInfos() (a, b *model.MovieInfo) {
	date := datatypes.Date(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	a = &model.MovieInfo{
		ID: "javbus-id", Number: "ABC-001", Provider: "JavBus",
		Homepage: "https://javbus.example/ABC-001",
		Title:    "javbus title", Summary: "",
		CoverURL: "https://javbus.example/cover.jpg",
		Actors:   pq.StringArray{"Actress A", "Actress B"},
		Genres:   pq.StringArray{"Genre 1"},
	}
	b = &model.MovieInfo{
		ID: "fanza-id", Number: "ABC-001", Provider: "FANZA",
		Homepage: "https://fanza.example/ABC-001",
		Title:    "fanza title", Summary: "fanza plot",
		CoverURL:    "https://fanza.example/cover.jpg",
		Genres:      pq.StringArray{"Genre 2", "Genre 3"},
		Runtime:     120,
		ReleaseDate: date,
	}
	return
}

func TestMergeMovieInfosDefaultPriority(t *testing.T) {
	a, b := sampleInfos()
	out := mergeMovieInfos([]*model.MovieInfo{a, b}, nil)

	// Identity from the first (highest-priority) provider.
	if out.Provider != "JavBus" || out.ID != "javbus-id" {
		t.Fatalf("identity should come from JavBus, got %q/%q", out.Provider, out.ID)
	}
	// Title: first non-empty wins (JavBus).
	if out.Title != "javbus title" {
		t.Fatalf("title = %q, want javbus title", out.Title)
	}
	// Summary: JavBus empty, so falls back to FANZA.
	if out.Summary != "fanza plot" {
		t.Fatalf("summary = %q, want fanza plot", out.Summary)
	}
	// Cover: first non-empty wins (JavBus).
	if out.CoverURL != "https://javbus.example/cover.jpg" {
		t.Fatalf("cover = %q", out.CoverURL)
	}
	// Runtime only FANZA has it.
	if out.Runtime != 120 {
		t.Fatalf("runtime = %d, want 120", out.Runtime)
	}
	// Actors only JavBus has it.
	if len(out.Actors) != 2 || out.Actors[0] != "Actress A" {
		t.Fatalf("actors = %v", out.Actors)
	}
	// ReleaseDate only FANZA has it.
	if time.Time(out.ReleaseDate).IsZero() {
		t.Fatalf("release date should be merged from FANZA")
	}
}

func TestMergeMovieInfosFieldPriority(t *testing.T) {
	a, b := sampleInfos()
	out := mergeMovieInfos([]*model.MovieInfo{a, b}, FieldPriority{
		"Title": {"FANZA"},
	})

	// FANZA is preferred for Title, overriding the default JavBus-first order.
	if out.Title != "fanza title" {
		t.Fatalf("title = %q, want fanza title", out.Title)
	}
	// Other fields keep the default order.
	if out.CoverURL != "https://javbus.example/cover.jpg" {
		t.Fatalf("cover = %q, want javbus cover", out.CoverURL)
	}
}

func TestChainForField(t *testing.T) {
	a, b := sampleInfos()
	c := &model.MovieInfo{Provider: "DUGA"}
	ordered := []*model.MovieInfo{a, b, c}

	chain := chainForField("Summary", ordered, FieldPriority{"Summary": {"DUGA", "FANZA"}})
	if len(chain) != 3 || chain[0].Provider != "DUGA" || chain[1].Provider != "FANZA" || chain[2].Provider != "JavBus" {
		t.Fatalf("unexpected chain order: %v", []string{chain[0].Provider, chain[1].Provider, chain[2].Provider})
	}

	// No preference keeps original order.
	chain = chainForField("Summary", ordered, nil)
	if chain[0].Provider != "JavBus" {
		t.Fatalf("nil priority should keep original order, got %q", chain[0].Provider)
	}
}
