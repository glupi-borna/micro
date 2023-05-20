package overlay

import (
	. "github.com/zyedidia/micro/v2/internal/loc"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/zyedidia/micro/v2/internal/util"
	"github.com/zyedidia/micro/v2/internal/screen"
	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/micro/v2/internal/buffer"
	"github.com/zyedidia/tcell/v2"
	"strings"
)

type BufWindow interface {
	CursorVisual() Loc
	IsActive() bool
	LocToVisual(int, int) Loc
}

// OpenBehavior describes What happens when opening an overlay
// when another overlay with the same ID already exists
type OpenBehavior int

const (
	// The new overlay is displayed alongside the old one
	OBAdd = iota
	// The new overlay is replaces the old one
	OBReplace
)

var GetCurrentBufWindow func() BufWindow

type OverlayPosition interface {
	ScreenPos() Loc
	Visible() bool
}

type V2 struct { Loc }

type Anchor struct {
	Window BufWindow
	loc Loc
}

type CursorAnchor struct {
	Window BufWindow
}

func (c CursorAnchor) ScreenPos() Loc {
	l := c.Window.CursorVisual()
	l.Y += 1
	return l
}

func (c CursorAnchor) Visible() bool {
	is_active := c.Window.IsActive()
	is_current := c.Window == GetCurrentBufWindow()
	return is_active && is_current
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
	CleanupHandler func(*Overlay)
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
	ID string, Window BufWindow, loc Loc, size Loc, ob OpenBehavior,
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
	ID string, Window BufWindow, size Loc, ob OpenBehavior,
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

func (o *Overlay) SetAnchor(Window BufWindow, loc Loc) {
	o.Pos = Anchor{Window, loc}
}

func (o *Overlay) SetPos(loc Loc) {
	o.Pos = V2{loc}
}

func (o *Overlay) SetCursorAnchor(Window BufWindow) {
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
func DrawText(text string, x1, y1, w, h int, style tcell.Style) int {
	tabsize := int(config.GlobalSettings["tabsize"].(float64))
	x := x1
	y := y1
	x2 := x1+w
	y2 := y1+h

	if y >= y2 { return 0 }

	DrawClear(x1, y, w, 1, style)

	for _, r := range text {
		rw := 1
		if r == '\t' {
			rw = tabsize
		} else {
			rw = runewidth.RuneWidth(r)
		}

		if r == '\n' || x+rw > x2 {
			x = x1
			y++
			if y < y2 {
				DrawClear(x1, y, w, 1, style)
			}
			if r == '\n' { continue }
		}
		if y >= y2 { break }

		screen.SetContent(x, y, r, nil, style)
		x += rw
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

func Text_MaxLine_TotalLines(s string) (int, int) {
	l := 0
	cur := 0
	lines := 1
	for _, ch := range s {
		if ch == '\n' {
			if cur > l { l = cur }
			cur = 0
			lines++
			continue
		}
		cur++
	}
	if cur > l { l = cur }
	return l, lines
}

func Text_Wrapped_MaxLineWidth_TotalLines(s string, maxwidth int) (string, int, int) {
	l := 0
	cur := 0
	lines := 1
	tabsize := int(config.GlobalSettings["tabsize"].(float64))
	tabstr := strings.Repeat(" ", tabsize)

	out := strings.Builder{}
	word := ""

	for _, ch := range s {
		switch ch {

		case '\n':
			// Flush word
			out.WriteString(word)
			word = ""

			// Update max line length and line count
			if cur > l { l = cur }
			cur = 0
			lines++

			// Insert newline char into string
			out.WriteRune(ch)
			continue

		case '\t':
			// Flush word
			out.WriteString(word)
			word = ""

			if cur + tabsize > maxwidth {
				// Update max line length and line count
				if cur > l { l = cur }
				cur = 0
				lines++
				// Insert newline char into string
				out.WriteRune('\n')
			}

			// Insert tab into string
			cur += tabsize
			out.WriteString(tabstr)
			continue

		default:
			rw := runewidth.RuneWidth(ch)
			ws := util.IsWhitespace(ch)

			if ws {
				// Flush word
				out.WriteString(word)
				word = ""
			} else {
				if len(word) + rw > maxwidth {
					// Flush word
					out.WriteString(word)
					word = ""

					// Update max line length and line count
					if cur > l { l = cur }
					cur = 0
					lines++

					// Insert newline char into string
					out.WriteRune('\n')
				}

				word += string(ch)
			}

			if cur + rw > maxwidth {
				// Update max line length and line count
				if cur > l { l = cur }
				cur = len(word)
				lines++
				// Insert newline char into string
				out.WriteRune('\n')
			} else if (ws) {
				out.WriteRune(ch)
			}

			cur += rw
		}
	}

	out.WriteString(word)
	if cur > l { l = cur }

	return out.String(), l, lines
}

func Tooltip(text string, op OverlayPosition) {
	maxw, lines := Text_MaxLine_TotalLines(text)
	wrapped, wraph := "", 0

	scroll := 0
	scrollSpeed := int(config.GlobalSettings["scrollspeed"].(float64))

	NewOverlay(
		"tooltip", op, Loc{maxw+2, lines}, OBReplace,

		func (o *Overlay) {
			wrapped, _, wraph = Text_Wrapped_MaxLineWidth_TotalLines(text, o.Size.X-2)
			o.Resize(maxw+2, wraph)

			style := config.DefStyle.Reverse(true)
			if s, ok := config.Colorscheme["tooltip"] ; ok {
				style = s
			}

			scrolled := strings.Join(strings.Split(wrapped, "\n")[scroll:], "\n")

			loc := o.ScreenPos()
			DrawClear(loc.X, loc.Y, o.Size.X, o.Size.Y, style)
			DrawText(scrolled, loc.X+1, loc.Y, o.Size.X-1, o.Size.Y, style)
		},

		func (o *Overlay, ev tcell.Event) bool {
			switch e := ev.(type) {
			case *tcell.EventKey:
				o.Remove()
				return false
			case *tcell.EventMouse:
				mx, my := e.Position()
				if o.Contains(mx, my) {
					b := e.Buttons()
					maxScroll := wraph - o.Size.Y + 1
					if wraph <= o.Size.Y {
						maxScroll = 0
					}

					if b == tcell.WheelUp {
						scroll = util.Clamp(scroll-scrollSpeed, 0, maxScroll)
						return true
					} else if b == tcell.WheelDown {
						scroll = util.Clamp(scroll+scrollSpeed, 0, maxScroll)
						return true
					}
				}
				o.Remove()
			}
			return false
		},
	)
}

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
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, rev)
				} else {
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, def)
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

func SearchMenu[K SelectOption](options []K, onSelect func(K), op OverlayPosition) {
	search_buffer := buffer.NewBufferFromString("", "", buffer.BTScratch)
	option := 0

	mx, my := 0, 0
	scroll := 0
	height := util.Min(len(options), 11)

	o := NewOverlay(
		"search_menu", op, Loc{20, height}, OBReplace,
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

			DrawText(search_buffer.Line(0), loc.X, loc.Y, o.Size.X, 1, def)

			x := loc.X
			y := loc.Y+1
			offset := 0

			for index:=0 ; index<util.Min(len(options)-scroll, 10) ; index++ {
				optindex := index + scroll
				opt := options[optindex]
				y_start := y + offset

				if optindex == option {
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, rev)
				} else {
					offset += DrawText(opt.Label(), x, y+offset, o.Size.X, o.Size.Y-offset, def)
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
				} else if e.Key() == tcell.KeyEnter {
					onSelect(options[option])
					o.Remove()
					return true
				} else if e.Key() == tcell.KeyRune {
					for _, c := range search_buffer.GetCursors() {
						search_buffer.SetCurCursor(c.Num)
						if c.HasSelection() {
							c.DeleteSelection()
							c.ResetSelection()
						}
						search_buffer.Insert(c.Loc, string(e.Rune()))
					}
					return true
				}

				// TODO: Extract bindings from action to a new module
			case *tcell.EventMouse:
				mx, my = e.Position()
				if !o.Contains(mx, my) { return false }
				b := e.Buttons()
				if my > o.Pos.ScreenPos().Y && b == tcell.Button1 {
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

	o.CleanupHandler = func(o *Overlay) {
		search_buffer.Close()
	}
}
