// Package fixture holds types for the tests of package apigen, which reads
// their doc comments and constants from this file
package fixture

import (
	"encoding/json"
	"time"
)

// Color is a color
type Color string

// Colors
const (
	// Red is warm
	Red   Color = "red"
	Green Color = "green" // Green is calm
	// DefaultColor is not a member: it has no type of its own
	DefaultColor = Red
)

// Shapes of things
const (
	ShapeRound  = "round"
	ShapeSquare = "square"
	// Shapes is no member: no upper-case letter follows the prefix
	Shapes = "shapes"
)

// Base is embedded
type Base struct {
	// ID identifies the thing
	ID string `json:"id"`
}

// Inner is referenced
type Inner struct {
	A string `json:"a"`
}

// Thing has a field of each kind
type Thing struct {
	Base
	Name    string `json:"name"`
	Opt     string `json:"opt,omitempty"` // Opt may be missing
	Ptr     *int   `json:"ptr"`
	PtrOpt  *int   `json:"ptrOpt,omitempty"`
	List    []string
	NonNull []Inner          `json:"nonNull"`
	Map     map[string]int   `json:"map,omitempty"`
	Struct  Inner            `json:"struct,omitempty"`
	Zero    Inner            `json:"zero,omitzero"`
	When    time.Time        `json:"when"`
	Color   Color            `json:"color"`
	Shape   string           `json:"shape,omitempty"`
	Shapes  []string         `json:"shapes,omitempty"`
	Pair    [2]float64       `json:"pair"`
	Any     any              `json:"any"`
	Raw     json.RawMessage  `json:"raw,omitempty"`
	Bytes   []byte           `json:"bytes,omitempty"`
	PtrList []*Inner         `json:"ptrList,omitempty"`
	Counts  Counts           `json:"counts"`
	Nested  map[string][]int `json:"nested,omitempty"`
	Skip    string           `json:"-"`
	hidden  string
}

// Counts is a named map
type Counts map[string]int64

// Request is sent by clients
type Request struct {
	Name  string   `json:"name"`
	Opt   *Inner   `json:"opt,omitempty"`
	Items []string `json:"items"`
	Inner Inner    `json:"inner,omitempty"`
}

// Point has its own JSON
type Point struct {
	X, Y float64
}

// MarshalJSON writes [x, y]
func (p Point) MarshalJSON() ([]byte, error) { return json.Marshal([2]float64{p.X, p.Y}) }

// WithPoint references a type with its own JSON
type WithPoint struct {
	P Point `json:"p"`
}

// Hidden embeds an unexported struct
type Hidden struct {
	hiddenBase
	Extra int `json:"extra"`
}

type hiddenBase struct {
	Inherited string `json:"inherited"`
}

// Collides has two fields with one JSON name
type Collides struct {
	Base
	Other string `json:"id"`
}

func (t Thing) unused() string { return t.hidden }

var _ = Thing.unused
