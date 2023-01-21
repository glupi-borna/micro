package display

import (
	"github.com/zyedidia/micro/v2/internal/util"
	"github.com/zyedidia/micro/v2/internal/screen"
	"github.com/zyedidia/micro/v2/internal/buffer"
	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/tcell/v2"
)

type Loc = buffer.Loc

// OpenBehavior describes What happens when opening an overlay
// when another overlay with the same ID already exists
type OpenBehavior int

const (
	// The new overlay is displayed alongside the old one
	OBAdd = iota
	// The new overlay is replaces the old one
	OBReplace
)

var GetCurrentBufWindow func() *BufWindow

type OverlayPosition interface {
	ScreenPos() Loc
	Visible() bool
}

type V2 struct { Loc }

type Anchor struct {
	Window *BufWindow
	loc Loc
}

type CursorAnchor struct {
	Window *BufWindow
}

func (c CursorAnchor) ScreenPos() Loc {
	l := c.Window.CursorVisual()
	l.Y += 1
	return l
}

func (c CursorAnchor) Visible() bool {
	return c.Window.IsActive() && GetCurrentBufWindow() == c.Window
}

func (a Anchor) ScreenPos() Loc {
	l := a.Window.LocToVisual(a.loc.X, a.loc.Y)
	l.Y += 1
	return l
}

func (a Anchor) Visible() bool {
	return a.Window.IsActive() && GetCurrentBufWindow() == a.Window
}

func (l V2) ScreenPos() Loc {
	return l.Loc
}

func (l V2) Visible() bool {
	return true
}

type Overlay struct {
	ID string
	Pos OverlayPosition
	Size Loc
	Draw func(*Overlay)
	EventHandler func(*Overlay, tcell.Event) bool
}

var Overlays = make(map[string][]*Overlay)

// Returns a slice of overlays with the given ID
func FindOverlays(ID string) []*Overlay {
	o, ok := Overlays[ID]
	if !ok { return nil }
	return o
}

func NewOverlay(
	ID string, pos OverlayPosition, size Loc, ob OpenBehavior,
	draw func(*Overlay),
	ev func(*Overlay, tcell.Event) bool,
) *Overlay {
	var o *Overlay

	switch ob {
	case OBAdd:
		o = new(Overlay)
	case OBReplace:
		RemoveOverlaysByID(ID)
		o = new(Overlay)
	}


	o.Pos = pos
	o.Resize(size.X, size.Y)
	o.ID = ID
	o.Draw = draw
	o.EventHandler = ev

	registerOverlay(o)

	return o
}

func NewOverlayAnchored(
	ID string, Window *BufWindow, loc Loc, size Loc, ob OpenBehavior,
	draw func(*Overlay),
	ev func(*Overlay, tcell.Event) bool,
) *Overlay {
	return NewOverlay(ID, Anchor{Window, loc}, size, ob, draw, ev)
}

func NewOverlayStatic(
	ID string, pos Loc, size Loc, ob OpenBehavior,
	draw func(*Overlay),
	ev func(*Overlay, tcell.Event) bool,
) *Overlay {
	return NewOverlay(ID, V2{pos}, size, ob, draw, ev)
}

func NewOverlayCursor(
	ID string, Window *BufWindow, size Loc, ob OpenBehavior,
	draw func(*Overlay),
	ev func(*Overlay, tcell.Event) bool,
) *Overlay {
	return NewOverlay(ID, CursorAnchor{Window}, size, ob, draw, ev)
}

// Removes a single specific overlay
func (o *Overlay) Remove() {
	id_overlays, ok := Overlays[o.ID]
	if !ok { return }
	for i, o2 := range id_overlays {
		if o2 == o {
			id_overlays[i] = id_overlays[len(id_overlays)-1]
			id_overlays[len(id_overlays)-1] = nil
			id_overlays = id_overlays[:len(id_overlays)-1]
			Overlays[o.ID] = id_overlays
			return
		}
	}
}

func (o *Overlay) Resize(width int, height int) {
	maxw, maxh := screen.Screen.Size()
	sp := o.ScreenPos()
	maxw = util.Max(maxw - sp.X, 0)
	maxh = util.Max(maxh - sp.Y, 0)

	o.Size.X = util.Min(width, maxw)
	o.Size.Y = util.Min(height, maxh)
}

func (o *Overlay) SetAnchor(Window *BufWindow, loc Loc) {
	o.Pos = Anchor{Window, loc}
}

func (o *Overlay) SetPos(loc Loc) {
	o.Pos = V2{loc}
}

func (o *Overlay) SetCursorAnchod(Window *BufWindow) {
	o.Pos = CursorAnchor{Window}
}

func (o *Overlay) HandleEvent(event tcell.Event) bool {
	if o.EventHandler != nil { return o.EventHandler(o, event) }
	return false
}

func registerOverlay(o *Overlay) {
	arr, ok := Overlays[o.ID]
	if !ok { arr = make([]*Overlay, 0) }
	arr = append(arr, o)
	Overlays[o.ID] = arr
}

// Removes all overlays with a given ID
func RemoveOverlaysByID(ID string) {
	delete(Overlays, ID)
}

// Completely removes all overlays
func RemoveAllOverlays() {
	Overlays = make(map[string][]*Overlay, len(Overlays))
}

// ScreenPos returns the screen-space coordinate of the
// anchor of the overlay.
func (o *Overlay) ScreenPos() Loc {
	return o.Pos.ScreenPos()
}

func (o *Overlay) Contains(x int, y int) bool {
	pos := o.ScreenPos()
	x_overlap := x >= pos.X && x <= pos.X + o.Size.X
	y_overlap := y >= pos.Y && y <= pos.Y + o.Size.Y
	return x_overlap && y_overlap
}

func (o *Overlay) Display() {
	o.Draw(o)
}

func DisplayOverlays() {
	for _, overlays := range Overlays {
		for _, overlay := range overlays {
			if !overlay.Pos.Visible() { continue }
			overlay.Display()
		}
	}
}

func HandleOverlayEvent(ev tcell.Event) bool {
	event_handled := false
	for _, overlays := range Overlays {
		for _, overlay := range overlays {
			if !overlay.Pos.Visible() { continue }
			event_handled = overlay.HandleEvent(ev)
			if event_handled { break }
		}
		if event_handled { break }
	}
	return event_handled
}

func DrawClear(x1, y1, w, h int, style tcell.Style) {
	x2 := x1+w
	y2 := y1+h
	for x := x1 ; x < x2 ; x++ {
		for y:= y1 ; y < y2; y++ {
			screen.SetContent(x, y, ' ', nil, style)
		}
	}
}

// Draws text, sized to the given rectangle, and returns the
// amount of lines required.
func DrawText(text string, x1, y1, w, h int, style tcell.Style, fillLine bool) int {
	x := x1
	y := y1
	x2 := x1+w
	y2 := y1+h

	if y >= y2 { return 0 }

	DrawClear(x1, y, w, 1, style)

	for _, r := range text {
		if r == '\n' || x >= x2 {
			x = x1
			y++
			if y < y2 { DrawClear(x1, y, w, 1, style) }
			if r == '\n' { continue }
		}
		if y >= y2 { break }
		screen.SetContent(x, y, r, nil, style)
		x++
	}

	return (y - y1) + 1
}

type SelectOption interface {
	Label() string
}

type SelectMenuOption[K any] struct {
	Value K
	Text string
}
func (m SelectMenuOption[any]) Label() string { return m.Text }

func SelectMenu[K SelectOption](options []K, onSelect func(K), op OverlayPosition) {
	option := 0
	mx, my := 0, 0

	scroll := 0
	height := util.Min(len(options), 10)

	NewOverlay(
		"select_menu", op, Loc{20, height}, OBReplace,
		func (o *Overlay) {
			loc := o.ScreenPos()
			DrawClear(loc.X, loc.Y, o.Size.X, o.Size.Y, tcell.StyleDefault)
			contains_mouse := o.Contains(mx, my)

			def := config.DefStyle.Reverse(true)
			rev := config.DefStyle
			if style, ok:= config.Colorscheme["statusline"]; ok {
				def = style
				rev = style.Reverse(true)
			}

			x := loc.X
			y := loc.Y
			offset := 0

			for index:=0 ; index<util.Min(len(options)-scroll, 10) ; index++ {
				optindex := index + scroll
				opt := options[optindex]
				y_start := y + offset

				if optindex == option {
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, rev, true)
				} else {
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, def, true)
				}

				if contains_mouse && my >= y_start && my < y+offset {
					contains_mouse = false
					option = optindex
					screen.Redraw()
				}
			}
		},
		func (o *Overlay, ev tcell.Event) bool {
			switch e := ev.(type) {
			case *tcell.EventKey:
				if e.Key() == tcell.KeyEnter {
					onSelect(options[option])
					o.Remove()
					return true
				} else if e.Key() == tcell.KeyUp {
					option = (option-1+len(options)) % len(options)
					scroll = util.Clamp(option-5, 0, len(options)-10)
					return true
				} else if e.Key() == tcell.KeyDown {
					option = (option+1) % len(options)
					scroll = util.Clamp(option-5, 0, len(options)-10)
					return true
				}
			case *tcell.EventMouse:
				mx, my = e.Position()
				if !o.Contains(mx, my) { return false }
				b := e.Buttons()
				if b == tcell.Button1 {
					onSelect(options[option])
					o.Remove()
				} else if b == tcell.WheelUp {
					scroll = util.Clamp(scroll-1, 0, len(options)-10)
				} else if b == tcell.WheelDown {
					scroll = util.Clamp(scroll+1, 0, len(options)-10)
				}
				return true
			}
			return false
		},
	)
}

