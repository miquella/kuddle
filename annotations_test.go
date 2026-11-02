package kuddle_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

var typeAnnotations = kuddle.WithUnmarshalers(kuddle.AllAnnotationUnmarshalers())

// annotated holds values that the reserved type annotations apply to, in
// each place a value can be.
type annotated struct {
	D  time.Duration   `kdl:"d"`
	DP *time.Duration  `kdl:"dp"`
	Ds []time.Duration `kdl:"ds"`
	T  time.Time       `kdl:"t"`
	S  string          `kdl:"s"`
	N  annotatedNode   `kdl:"n"`
}

type annotatedNode struct {
	T time.Time     `kdl:",arg"`
	D time.Duration `kdl:"d,prop"`
}

// utc returns a with its times in UTC, to compare them by reflect.DeepEqual.
func (a annotated) utc() annotated {
	a.T, a.N.T = a.T.UTC(), a.N.T.UTC()
	return a
}

// TestAnnotations checks that values annotated (duration), (date-time) and
// (date) are decoded into the Go types the annotations describe, and that
// other annotations, and the reserved ones on other Go types, are not
// interpreted.
func TestAnnotations(t *testing.T) {
	ptr := func(d time.Duration) *time.Duration { return &d }
	tests := []struct {
		input string
		want  annotated
	}{
		{`n d=(duration)"PT1H30M"`, annotated{N: annotatedNode{D: 90 * time.Minute}}},
		{`d (duration)"PT0.5S"`, annotated{D: 500 * time.Millisecond}},
		{`dp (duration)"PT2M"`, annotated{DP: ptr(2 * time.Minute)}},
		{`ds (duration)"PT1S" 5 (duration)"-PT1M"`, annotated{Ds: []time.Duration{time.Second, 5, -time.Minute}}},
		{`d 30`, annotated{D: 30}},
		{`d (duration)#null`, annotated{}},
		{`t (date-time)"2026-11-02T08:30:00-07:00"`, annotated{T: time.Date(2026, 11, 2, 8, 30, 0, 0, time.FixedZone("", -7*3600))}},
		{`n (date)"2026-11-02"`, annotated{N: annotatedNode{T: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}},
		{`t "2026-11-02T15:30:00Z"`, annotated{T: time.Date(2026, 11, 2, 15, 30, 0, 0, time.UTC)}},
		{`s (duration)"PT1S"`, annotated{S: "PT1S"}},
		{`s (date)"not a date"`, annotated{S: "not a date"}},
	}
	for _, tt := range tests {
		var got annotated
		if err := kuddle.Unmarshal([]byte(tt.input), &got, typeAnnotations); err != nil {
			t.Errorf("Unmarshal(%q): %v", tt.input, err)
			continue
		}
		if !reflect.DeepEqual(got.utc(), tt.want.utc()) {
			t.Errorf("Unmarshal(%q):\ngot  %+v\nwant %+v", tt.input, got, tt.want)
		}
		checkTo[annotated](t, tt.input, typeAnnotations)
	}
}

// TestAnnotationErrors checks the errors for annotated values that don't
// have the forms their annotations reserve.
func TestAnnotationErrors(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`d (duration)30`, "expected a string, found number 30"},
		{`d (duration)"30s"`, `invalid ISO 8601 duration "30s": want it to start with P`},
		{`d (duration)"P1D"`, "years, months, weeks and days have no fixed length"},
		{`d (duration)"P1DT1H"`, "years, months, weeks and days have no fixed length"},
		{`d (duration)"P"`, "want hours, minutes or seconds after PT"},
		{`d (duration)"PT"`, "want hours, minutes or seconds after PT"},
		{`d (duration)"PT1"`, "want H, M or S after a number"},
		{`d (duration)"PTS"`, "want a number"},
		{`d (duration)"PT1S1M"`, "want H, M and S, each at most once and in that order"},
		{`d (duration)"PT1H1H"`, "want H, M and S, each at most once and in that order"},
		{`d (duration)"PT1.5H"`, "only the seconds may have a fraction"},
		{`d (duration)"PT1.S"`, "want digits after the decimal sign"},
		{`d (duration)"PT0.0000000001S"`, "more precise than a nanosecond"},
		{`d (duration)"PT9999999999999H"`, "out of range"},
		{`d (duration)"PT2562047H47M17S"`, "out of range"},
		{`t (date-time)"2026-11-02"`, "cannot parse"},
		{`t (date)"2026-11-02T15:30:00Z"`, `invalid date "2026-11-02T15:30:00Z", want the form 2006-01-02`},
		{`t (duration)"PT1S"`, "cannot parse"},
	}
	for _, tt := range tests {
		var got annotated
		err := kuddle.Unmarshal([]byte(tt.input), &got, typeAnnotations)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Unmarshal(%q): got error %v, want one containing %q", tt.input, err, tt.want)
		}
		checkTo[annotated](t, tt.input, typeAnnotations)
	}
}

// TestAnnotationParse checks the forms of ISO 8601 durations that are read.
func TestAnnotationParse(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"PT0S", 0},
		{"PT1H", time.Hour},
		{"PT1M", time.Minute},
		{"PT1H2M3S", time.Hour + 2*time.Minute + 3*time.Second},
		{"PT1H3S", time.Hour + 3*time.Second},
		{"PT90M", 90 * time.Minute},
		{"PT1,25S", 1250 * time.Millisecond},
		{"PT0.000000001S", time.Nanosecond},
		{"-PT1H", -time.Hour},
		{"PT2562047H47M16.854775807S", time.Duration(1<<63 - 1)},
	}
	for _, tt := range tests {
		var got struct {
			D time.Duration `kdl:"d"`
		}
		input := `d (duration)"` + tt.in + `"`
		if err := kuddle.Unmarshal([]byte(input), &got, typeAnnotations); err != nil {
			t.Errorf("%s: %v", tt.in, err)
		} else if got.D != tt.want {
			t.Errorf("%s: got %v, want %v", tt.in, got.D, tt.want)
		}
	}
}

// TestAnnotationSubset checks that AnnotationUnmarshalers returns only the
// unmarshalers of the annotations it is given, and panics for one it has
// none for.
func TestAnnotationSubset(t *testing.T) {
	durations := kuddle.WithUnmarshalers(kuddle.AnnotationUnmarshalers("duration"))
	var got annotated
	if err := kuddle.Unmarshal([]byte(`d (duration)"PT1M"`), &got, durations); err != nil || got.D != time.Minute {
		t.Errorf("(duration): got %v, %v", got.D, err)
	}
	if err := kuddle.Unmarshal([]byte(`t (date)"2026-11-02"`), &got, durations); err == nil {
		t.Errorf("(date) without its unmarshaler: got %v, want an error", got.T)
	}
	none := kuddle.WithUnmarshalers(kuddle.AnnotationUnmarshalers())
	if err := kuddle.Unmarshal([]byte(`d (duration)"PT1M"`), &got, none); err == nil {
		t.Errorf("(duration) with no unmarshalers: got %v, want an error", got.D)
	}

	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), `"no-such-annotation"`) {
			t.Errorf("got panic %v, want one for \"no-such-annotation\"", r)
		}
	}()
	kuddle.AnnotationUnmarshalers("duration", "no-such-annotation")
}

// TestAnnotationOrder checks that the annotations are interpreted only with
// AllAnnotationUnmarshalers, and in the order it is joined with other
// functions.
func TestAnnotationOrder(t *testing.T) {
	// length reads any string as a Duration of a second per character.
	length := kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		*d = time.Duration(len(tok.Value.String())) * time.Second
		return nil
	})
	type doc struct {
		D []time.Duration `kdl:"d"`
	}
	const input = `d (duration)"PT1H" "xy"`
	tests := []struct {
		opts []kuddle.Options
		want []time.Duration
		err  string
	}{
		{nil, nil, "expected a number, found string"},
		{[]kuddle.Options{kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(length, kuddle.AllAnnotationUnmarshalers()))}, []time.Duration{4 * time.Second, 2 * time.Second}, ""},
		{[]kuddle.Options{kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(kuddle.AllAnnotationUnmarshalers(), length))}, []time.Duration{time.Hour, 2 * time.Second}, ""},
	}
	for i, tt := range tests {
		var got doc
		err := kuddle.Unmarshal([]byte(input), &got, tt.opts...)
		switch {
		case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
			t.Errorf("%d: got error %v, want one containing %q", i, err, tt.err)
		case tt.err == "" && err != nil:
			t.Errorf("%d: %v", i, err)
		case tt.err == "" && !reflect.DeepEqual(got.D, tt.want):
			t.Errorf("%d: got %v, want %v", i, got.D, tt.want)
		}
	}
}
