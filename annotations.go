package kuddle

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/miquella/kuddle/kdltext"
)

// AnnotationUnmarshalers returns built-in unmarshalers, for use with
// WithUnmarshalers, for the named type annotations. These are among the
// annotations the KDL spec reserves:
//
//   - "date-time": an RFC 3339 date and time, into a time.Time.
//   - "date": a date, such as (date)"2006-01-02", into a time.Time at
//     midnight UTC.
//   - "duration": an ISO 8601 duration of hours, minutes and seconds, such as
//     (duration)"PT1H30M" or "-PT0.5S", into a time.Duration.
//
// With no annotations, it returns no unmarshalers. It panics if it has no
// unmarshaler for an annotation. [AllAnnotationUnmarshalers] returns all of
// them.
//
// Each unmarshaler reads only values with its annotation. For any other
// value, it returns errors.ErrUnsupported without reading anything, so the
// value is unmarshaled as if the unmarshaler weren't there. Joined after a
// caller's own functions, as in
//
//	kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(mine, kuddle.AnnotationUnmarshalers("duration")))
//
// they handle the values those functions pass on. Marshal doesn't write type
// annotations for values, but a function passed with WithMarshalers can.
func AnnotationUnmarshalers(annotations ...string) *Unmarshalers {
	us := make([]*Unmarshalers, len(annotations))
	for i, a := range annotations {
		u, ok := annotationUnmarshalers[a]
		if !ok {
			panic(fmt.Sprintf("kuddle: no unmarshaler for the type annotation %q", a))
		}
		us[i] = u
	}
	return JoinUnmarshalers(us...)
}

// AllAnnotationUnmarshalers returns every built-in unmarshaler that
// AnnotationUnmarshalers provides. Later versions may add unmarshalers for
// more of the annotations the KDL spec reserves, so this set can grow. To
// keep a fixed set, name the annotations with AnnotationUnmarshalers.
func AllAnnotationUnmarshalers() *Unmarshalers { return allAnnotationUnmarshalers }

// annotationUnmarshalers are the built-in unmarshalers, by type annotation.
var annotationUnmarshalers = map[string]*Unmarshalers{
	"date-time": UnmarshalFunc(func(dec *kdltext.Decoder, t *time.Time) error {
		s, err := annotated(dec, "date-time")
		if err != nil {
			return err
		}
		return t.UnmarshalText([]byte(s))
	}),
	"date": UnmarshalFunc(func(dec *kdltext.Decoder, t *time.Time) error {
		s, err := annotated(dec, "date")
		if err != nil {
			return err
		}
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return fmt.Errorf("invalid date %q, want the form 2006-01-02", s)
		}
		*t = d
		return nil
	}),
	"duration": UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
		s, err := annotated(dec, "duration")
		if err != nil {
			return err
		}
		*d, err = parseISODuration(s)
		return err
	}),
}

// allAnnotationUnmarshalers is all of annotationUnmarshalers, in the order
// that the KDL spec lists the annotations.
var allAnnotationUnmarshalers = JoinUnmarshalers(
	annotationUnmarshalers["date-time"],
	annotationUnmarshalers["date"],
	annotationUnmarshalers["duration"],
)

// annotated reads the next token, a string value with the type annotation
// typ, and returns the string. It returns errors.ErrUnsupported without
// reading anything if the next token is not a value with the annotation, or
// is #null, which the default rules decode.
func annotated(dec *kdltext.Decoder, typ string) (string, error) {
	t, err := dec.PeekToken()
	if err != nil {
		return "", err
	}
	val := t.Value
	if t.Kind != kdltext.KindValue || val.Type == nil || *val.Type != typ || val.Kind() == kdltext.ValueNull {
		return "", errors.ErrUnsupported
	}
	if val.Kind() != kdltext.ValueString {
		return "", mismatch(val, "a string")
	}
	dec.ReadToken()
	return val.String(), nil
}

// parseISODuration parses an ISO 8601 duration of hours, minutes and seconds,
// such as PT1H30M or PT0.5S, negated by a leading '-'. Years, months, weeks
// and days are rejected, since their lengths vary. Only the seconds may have
// a fraction, of up to nine digits, after a '.' or ','.
func parseISODuration(s string) (time.Duration, error) {
	in := s
	invalid := func(why string) (time.Duration, error) {
		return 0, fmt.Errorf("invalid ISO 8601 duration %q: %s", in, why)
	}
	s, neg := strings.CutPrefix(s, "-")
	s, ok := strings.CutPrefix(s, "P")
	if !ok {
		return invalid("want it to start with P")
	}
	if date, _, _ := strings.Cut(s, "T"); strings.ContainsAny(date, "YMWD") {
		return invalid("years, months, weeks and days have no fixed length")
	}
	s, ok = strings.CutPrefix(s, "T")
	if !ok || s == "" {
		return invalid("want hours, minutes or seconds after PT")
	}

	units := []struct {
		designator byte
		size       time.Duration
	}{{'H', time.Hour}, {'M', time.Minute}, {'S', time.Second}}
	var total time.Duration
	for s != "" {
		i := digits(s)
		if i == 0 {
			return invalid("want a number")
		}
		whole, frac := s[:i], ""
		if i < len(s) && (s[i] == '.' || s[i] == ',') {
			n := digits(s[i+1:])
			if n == 0 {
				return invalid("want digits after the decimal sign")
			}
			frac = s[i+1 : i+1+n]
			i += 1 + n
		}
		if i == len(s) {
			return invalid("want H, M or S after a number")
		}
		d := s[i]
		s = s[i+1:]
		for len(units) > 0 && units[0].designator != d {
			units = units[1:]
		}
		if len(units) == 0 {
			return invalid("want H, M and S, each at most once and in that order")
		}
		size := units[0].size
		units = units[1:]

		n, err := strconv.ParseInt(whole, 10, 64)
		if err != nil || n > math.MaxInt64/int64(size) {
			return invalid("out of range")
		}
		part := time.Duration(n) * size
		if frac != "" {
			if d != 'S' {
				return invalid("only the seconds may have a fraction")
			}
			if len(frac) > 9 {
				return invalid("more precise than a nanosecond")
			}
			ns, _ := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
			if part > math.MaxInt64-time.Duration(ns) {
				return invalid("out of range")
			}
			part += time.Duration(ns)
		}
		if total > math.MaxInt64-part {
			return invalid("out of range")
		}
		total += part
	}
	if neg {
		total = -total
	}
	return total, nil
}

// digits returns the number of ASCII digits at the start of s.
func digits(s string) int {
	i := 0
	for i < len(s) && '0' <= s[i] && s[i] <= '9' {
		i++
	}
	return i
}
