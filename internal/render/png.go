package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"

	"github.com/wuhanhao456/quota-cli/internal/provider"
)

var (
	//go:embed assets/wu.png
	defaultAvatar []byte
	//go:embed assets/Cinzel-Bold.ttf
	cinzelBold []byte
	//go:embed assets/WuSans-SC.ttf
	sansSC []byte
	//go:embed assets/WuSerif-SC.ttf
	serifSC []byte
)

// Persona decorates the image with a character portrait, a name, a motto and a colour theme.
type Persona struct {
	Avatar  string // image file (PNG/JPEG); "" uses the built-in portrait, "none" hides the portrait panel
	Name    string
	Tagline string
	Theme   string // "auto" (default: night from 18:00 to 06:00 local time), "day" or "night"
}

// DefaultPersona is the built-in character, Wu.
var DefaultPersona = Persona{Name: "WU", Tagline: "平静的心，更广阔的世界。", Theme: "auto"}

// Themes lists the accepted values of Persona.Theme.
var Themes = []string{"auto", "day", "night"}

// The image is laid out in logical pixels and drawn at scale for sharp text on HiDPI screens.
const (
	scale      = 2.0
	imgWidth   = 1000.0
	margin     = 40.0
	panelW     = 290.0
	panelGap   = 28.0
	minPanelH  = 760.0
	cardPad    = 22.0
	cardGap    = 16.0
	headerH    = 100.0
	cardHeadH  = 60.0
	windowRowH = 64.0
	detailRowH = 30.0
	messageH   = 40.0
	footerH    = 46.0
	calloutH   = 92.0
)

// A palette is one look for the image. Both are taken from Wu's art: "day" from the
// porcelain character sheet, "night" from the moonlit cornflower field.
type palette struct {
	dark                                       bool
	bgTop, bgBottom, frame, corner             color.Color
	panelTop, panelMid, panelBottom, halo      color.Color
	card, border, hairline, text, muted, track color.Color
	sky, cobalt, royal, navy, gold, rose       color.Color
	flower, flowerLight, pace                  color.Color
	shadowAlpha                                float64
}

var dayTheme = palette{
	bgTop: hex("#FBFCFF"), bgBottom: hex("#EDF2FD"), frame: hex("#4E89FB"), corner: hex("#4E89FB"),
	panelTop: hex("#FFFFFF"), panelMid: hex("#F1F5FE"), panelBottom: hex("#DCE6FC"), halo: hex("#BED0F8"),
	card: hex("#FFFFFF"), border: hex("#D3E0FA"), hairline: hex("#8FB0F5"),
	text: hex("#1E2550"), muted: hex("#6E7BA3"), track: hex("#E7EEFC"),
	sky: hex("#BED0F8"), cobalt: hex("#4E89FB"), royal: hex("#244092"), navy: hex("#212952"),
	gold: hex("#B8925A"), rose: hex("#D45B7A"),
	flower: hex("#3F6FE8"), flowerLight: hex("#8FB0F5"), pace: hex("#212952"),
	shadowAlpha: 0.05,
}

var nightTheme = palette{
	dark:  true,
	bgTop: hex("#0F2350"), bgBottom: hex("#07122C"), frame: hex("#6E95E8"), corner: hex("#E3C489"),
	panelTop: hex("#16295A"), panelMid: hex("#102049"), panelBottom: hex("#0B1838"), halo: hex("#C9DAFF"),
	card: hex("#12224A"), border: hex("#28407A"), hairline: hex("#5C82D6"),
	text: hex("#DCE5FF"), muted: hex("#8C9DCB"), track: hex("#1F3263"),
	sky: hex("#3A63C4"), cobalt: hex("#6FA3FF"), royal: hex("#B4C8FF"), navy: hex("#F2F5FF"),
	gold: hex("#E3C489"), rose: hex("#FF7E9C"),
	flower: hex("#4F7BF0"), flowerLight: hex("#9DB9FF"), pace: hex("#F2F5FF"),
	shadowAlpha: 0.25,
}

func themeFor(name string, now time.Time) (palette, error) {
	switch strings.ToLower(name) {
	case "", "auto":
		if h := now.Local().Hour(); h >= 18 || h < 6 {
			return nightTheme, nil
		}
		return dayTheme, nil
	case "day", "light":
		return dayTheme, nil
	case "night", "dark":
		return nightTheme, nil
	}
	return palette{}, fmt.Errorf("unknown theme %q (want %s)", name, strings.Join(Themes, ", "))
}

var accents = map[string]string{
	"claude":      "#D97757",
	"chatgpt":     "#10A37F",
	"commandcode": "#8B7CF6",
	"ollama":      "#5B6478",
	"opencode":    "#3D4451",
}

// Grey brand colours vanish on the night sky, so lift them there.
var nightAccents = map[string]string{"ollama": "#AEB7CC", "opencode": "#CDD3E0"}

func (t palette) accent(id string) color.Color {
	if s, ok := nightAccents[id]; ok && t.dark {
		return hex(s)
	}
	if s, ok := accents[id]; ok {
		return hex(s)
	}
	return t.cobalt
}

// Latin display text (her name, provider names, big numbers) is set in Cinzel, like the
// character sheet; Chinese headings and the motto in a Song serif (Source Han Serif), and
// the data in a clean sans (Noto Sans CJK), whose Latin digits read well at small sizes.
type fonts struct {
	display, displayZH, title, name, label, value, small, badge, motto, caps, big font.Face
}

func loadFonts() fonts {
	parsed := map[*byte]*truetype.Font{}
	face := func(ttf []byte, size float64) font.Face {
		f := parsed[&ttf[0]]
		if f == nil {
			var err error
			if f, err = truetype.Parse(ttf); err != nil {
				panic(err) // embedded fonts are known-good
			}
			parsed[&ttf[0]] = f
		}
		ff := truetype.NewFace(f, &truetype.Options{Size: size * scale, DPI: 72, Hinting: font.HintingFull})
		// A fresh face reports a zero advance for the first glyph it measures; warm it up
		// so letter-spaced text measures correctly from the first rune.
		font.MeasureString(ff, "M")
		return ff
	}
	return fonts{
		display:   face(cinzelBold, 54),
		displayZH: face(serifSC, 46),
		title:     face(serifSC, 26),
		name:      face(cinzelBold, 17),
		label:     face(sansSC, 14),
		value:     face(sansSC, 14),
		small:     face(sansSC, 12),
		badge:     face(sansSC, 11),
		motto:     face(serifSC, 17),
		caps:      face(serifSC, 11.5),
		big:       face(cinzelBold, 26),
	}
}

// painter carries the canvas, fonts, theme and clock through one render.
type painter struct {
	dc  *gg.Context
	f   fonts
	t   palette
	now time.Time
}

func cardHeight(r provider.Report) float64 {
	h := cardHeadH + cardPad/2
	if r.Status != "ok" {
		return h + messageH
	}
	n := len(r.Windows)
	if n == 0 && len(r.Details) == 0 {
		return h + messageH
	}
	return h + float64(n)*windowRowH + float64(len(r.Details))*detailRowH
}

// PNG renders the report as a dashboard-style image and saves it to path.
func PNG(path string, out Output, p Persona) error {
	return renderPNG(path, out, p, time.Now())
}

func renderPNG(path string, out Output, p Persona, now time.Time) error {
	t, err := themeFor(p.Theme, now)
	if err != nil {
		return err
	}
	avatar, err := loadAvatar(p.Avatar)
	if err != nil {
		return err
	}
	withPanel := avatar != nil

	colX := margin
	if withPanel {
		colX += panelW + panelGap
	}
	colW := imgWidth - margin - colX

	content := headerH
	for _, r := range out.Providers {
		content += cardHeight(r) + cardGap
	}
	content -= cardGap
	if withPanel {
		content = max(content, minPanelH)
	}
	total := margin + content + footerH + margin/2

	pt := &painter{dc: gg.NewContext(int(imgWidth*scale), int(total*scale)), f: loadFonts(), t: t, now: now}
	dc, f := pt.dc, pt.f

	pt.backdrop(total)
	if withPanel {
		pt.persona(p, avatar, tightest(out, now), margin, margin, panelW, content)
	}

	// Header: title, generation time and an overall summary, over a gold hem border.
	dc.SetFontFace(f.title)
	dc.SetColor(t.navy)
	title := "订阅额度"
	if !withPanel && p.Name != "" {
		title = p.Name + " · " + title
	}
	drawBold(func(dx float64) { drawSpaced(dc, title, colX+dx, margin+32, 4, 0) })
	dc.SetFontFace(f.small)
	dc.SetColor(t.muted)
	dc.DrawString(summary(out)+"  ·  "+stamp(out, now).Format("2006-01-02 15:04 MST"), colX*scale, (margin+56)*scale)
	pt.hemBorder(colX, margin+76, colW)

	y := margin + headerH
	for _, r := range out.Providers {
		h := cardHeight(r)
		pt.card(r, colX, y, colW, h)
		y += h + cardGap
	}

	fy := margin + content + footerH/2 + 4
	dc.SetFontFace(f.caps)
	dc.SetColor(t.muted)
	gw := drawSpaced(dc, "由 quota-cli 生成", imgWidth-margin, fy, 1.4, 1)
	// Legend for the time marker on the bars.
	legend := "金色刻度：本周期已过去的时间"
	dc.SetFontFace(f.small)
	lw, _ := dc.MeasureString(legend)
	lx := imgWidth - margin - gw - 28 - lw/scale
	dc.SetColor(t.muted)
	dc.DrawStringAnchored(legend, (lx+8)*scale, (fy-4)*scale, 0, 0.35)
	pt.paceMark(lx, fy-9, 10)
	if !withPanel && p.Tagline != "" {
		dc.SetFontFace(f.motto)
		dc.SetColor(t.royal)
		dc.DrawStringAnchored(p.Tagline, colX*scale, fy*scale, 0, 0.35)
	}
	return dc.SavePNG(path)
}

// loadAvatar returns the portrait to draw, or nil when the panel is disabled.
func loadAvatar(path string) (image.Image, error) {
	switch strings.ToLower(path) {
	case "none", "off":
		return nil, nil
	case "":
		return png.Decode(bytes.NewReader(defaultAvatar))
	}
	fh, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("avatar: %w", err)
	}
	defer fh.Close()
	img, _, err := image.Decode(fh)
	if err != nil {
		return nil, fmt.Errorf("avatar %s: %w", path, err)
	}
	return img, nil
}

// backdrop paints the page, its double frame with corner stars, and on the night theme
// a field of stars and the moon.
func (pt *painter) backdrop(total float64) {
	dc, t := pt.dc, pt.t
	g := gg.NewLinearGradient(0, 0, 0, total*scale)
	g.AddColorStop(0, t.bgTop)
	g.AddColorStop(1, t.bgBottom)
	dc.SetFillStyle(g)
	dc.DrawRectangle(0, 0, imgWidth*scale, total*scale)
	dc.Fill()

	if t.dark {
		rng := rand.New(rand.NewSource(147)) // fixed seed: the same sky every time
		for i := 0; i < 220; i++ {
			x, y := rng.Float64()*imgWidth, rng.Float64()*total
			a := 0.15 + rng.Float64()*0.55
			c := hex("#FFFFFF")
			if rng.Intn(5) == 0 {
				c = hex("#9DB9FF")
			}
			dc.SetColor(withAlpha(c, a))
			dc.DrawCircle(x*scale, y*scale, (0.4+rng.Float64()*0.9)*scale)
			dc.Fill()
		}
		for i := 0; i < 9; i++ {
			x, y := rng.Float64()*imgWidth, rng.Float64()*total
			dc.SetColor(withAlpha(hex("#CFE0FF"), 0.5+rng.Float64()*0.4))
			star(dc, x, y, 2.5+rng.Float64()*3)
		}
		pt.moon(imgWidth-margin-70, margin+40, 20)
	}

	for i, in := range []float64{14, 18} {
		dc.SetColor(withAlpha(t.frame, []float64{0.55, 0.25}[i]))
		dc.SetLineWidth(0.8 * scale)
		dc.DrawRectangle(in*scale, in*scale, (imgWidth-2*in)*scale, (total-2*in)*scale)
		dc.Stroke()
	}
	for _, c := range [][2]float64{{16, 16}, {imgWidth - 16, 16}, {16, total - 16}, {imgWidth - 16, total - 16}} {
		dc.SetColor(t.bgTop)
		if c[1] > 16 {
			dc.SetColor(t.bgBottom)
		}
		dc.DrawCircle(c[0]*scale, c[1]*scale, 6*scale)
		dc.Fill()
		dc.SetColor(t.corner)
		star(dc, c[0], c[1], 7)
	}
}

// moon draws a pale full moon with a soft glow.
func (pt *painter) moon(cx, cy, r float64) {
	dc := pt.dc
	glow := gg.NewRadialGradient(cx*scale, cy*scale, r*scale, cx*scale, cy*scale, r*4*scale)
	glow.AddColorStop(0, withAlpha(hex("#FFF6D8"), 0.28))
	glow.AddColorStop(1, withAlpha(hex("#FFF6D8"), 0))
	dc.SetFillStyle(glow)
	dc.DrawCircle(cx*scale, cy*scale, r*4*scale)
	dc.Fill()
	body := gg.NewRadialGradient((cx-r*0.3)*scale, (cy-r*0.3)*scale, 0, cx*scale, cy*scale, r*scale)
	body.AddColorStop(0, hex("#FFFBEA"))
	body.AddColorStop(1, hex("#EADFB8"))
	dc.SetFillStyle(body)
	dc.DrawCircle(cx*scale, cy*scale, r*scale)
	dc.Fill()
	dc.SetColor(withAlpha(hex("#C9BC93"), 0.35))
	for _, m := range [][3]float64{{-0.3, -0.2, 0.22}, {0.25, 0.15, 0.16}, {-0.05, 0.4, 0.12}} {
		dc.DrawCircle((cx+m[0]*r)*scale, (cy+m[1]*r)*scale, m[2]*r*scale)
		dc.Fill()
	}
}

// low is the window with the least headroom, shown under the portrait.
type low struct {
	provider string
	win      provider.Window
}

func tightest(out Output, now time.Time) *low {
	var best *low
	for _, r := range out.Providers {
		if r.Status != "ok" {
			continue
		}
		for _, w := range r.Windows {
			if w.ResetsAt != nil && !w.ResetsAt.After(now) {
				continue // already reset; the number is stale
			}
			if best == nil || w.UsedPercent > best.win.UsedPercent {
				best = &low{r.Name, w}
			}
		}
	}
	return best
}

func (pt *painter) persona(p Persona, avatar image.Image, tight *low, x, y, w, h float64) {
	dc, f, t := pt.dc, pt.f, pt.t
	g := gg.NewLinearGradient(0, y*scale, 0, (y+h)*scale)
	g.AddColorStop(0, t.panelTop)
	g.AddColorStop(0.55, t.panelMid)
	g.AddColorStop(1, t.panelBottom)
	dc.SetFillStyle(g)
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 16*scale)
	dc.Fill()
	dc.SetColor(t.border)
	dc.SetLineWidth(1 * scale)
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 16*scale)
	dc.Stroke()
	// Inner hairline, like the inset frame on the character sheet.
	dc.SetColor(withAlpha(t.gold, 0.45))
	dc.SetLineWidth(0.6 * scale)
	dc.DrawRoundedRectangle((x+6)*scale, (y+6)*scale, (w-12)*scale, (h-12)*scale, 12*scale)
	dc.Stroke()

	cx := x + w/2
	top := y + 26
	if p.Name != "" {
		dc.SetColor(t.royal)
		if isASCII(p.Name) {
			dc.SetFontFace(f.display)
			drawSpaced(dc, strings.ToUpper(p.Name), cx, top+44, 4, 0.5)
		} else {
			dc.SetFontFace(f.displayZH)
			drawBold(func(dx float64) { drawSpaced(dc, p.Name, cx+dx, top+44, 6, 0.5) })
		}
		top += 62
	}
	// Ornamental divider: hairline, diamond, hairline.
	dc.SetColor(t.royal)
	dc.SetLineWidth(0.8 * scale)
	dc.DrawLine((cx-62)*scale, top*scale, (cx-8)*scale, top*scale)
	dc.DrawLine((cx+8)*scale, top*scale, (cx+62)*scale, top*scale)
	dc.Stroke()
	diamond(dc, cx, top, 4.5, false)
	diamond(dc, cx, top, 1.8, true)
	dc.SetFontFace(f.caps)
	dc.SetColor(t.cobalt)
	drawSpaced(dc, "额 度 账 本", cx, top+21, 2.4, 0.5)
	top += 34

	// Reserve the bottom for the "lowest remaining" callout and the motto.
	bottomH := 0.0
	if tight != nil {
		bottomH += calloutH
	}
	if p.Tagline != "" {
		bottomH += 40
	}
	by := y + h - 18 - bottomH // the callout and motto sit at the bottom of the panel
	boxW, boxH := w-16, by-16-top
	b := avatar.Bounds()
	k := math.Min(boxW/float64(b.Dx()), boxH/float64(b.Dy()))
	aw, ah := float64(b.Dx())*k, float64(b.Dy())*k
	ax, ay := cx-aw/2, top+(boxH-ah)/2
	feet := ay + ah

	// Soft halo (moonlight at night) and a few sparkles behind her.
	hy := ay + ah*0.25
	halo := gg.NewRadialGradient(cx*scale, hy*scale, 0, cx*scale, hy*scale, w*0.58*scale)
	ha := 0.55
	if t.dark {
		ha = 0.22
	}
	halo.AddColorStop(0, withAlpha(t.halo, ha))
	halo.AddColorStop(1, withAlpha(t.halo, 0))
	dc.SetFillStyle(halo)
	dc.DrawCircle(cx*scale, hy*scale, w*0.58*scale)
	dc.Fill()
	dc.SetColor(withAlpha(t.cobalt, 0.7))
	for _, s := range [][3]float64{{-0.40, 0.08, 6}, {0.42, 0.20, 4.5}, {-0.40, 0.50, 3.5}, {0.40, 0.58, 5}} {
		star(dc, cx+s[0]*w, ay+s[1]*ah, s[2])
	}
	dc.SetColor(withAlpha(t.royal, 0.12))
	dc.DrawEllipse(cx*scale, (feet-3)*scale, aw*0.40*scale, 5*scale)
	dc.Fill()
	pt.flowers(cx, feet, w, false)

	dst := image.NewRGBA(image.Rect(0, 0, int(aw*scale), int(ah*scale)))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), avatar, b, xdraw.Over, nil)
	dc.DrawImage(dst, int(ax*scale), int(ay*scale))
	pt.flowers(cx, feet, w, true)

	if tight != nil {
		pt.callout(tight, x+18, by, w-36)
		by += calloutH
	}
	if p.Tagline != "" {
		dc.SetFontFace(f.motto)
		dc.SetColor(t.royal)
		dc.DrawStringAnchored(truncate(dc, p.Tagline, (w-24)*scale), cx*scale, (by+22)*scale, 0.5, 0)
	}
}

// flowers scatters cornflowers (her scent) around her feet: a back row behind the
// portrait and a few in front.
func (pt *painter) flowers(cx, base, w float64, front bool) {
	type fl struct{ dx, dy, r float64 }
	back := []fl{{-0.36, -10, 5.5}, {-0.27, -4, 4}, {0.30, -8, 5}, {0.39, -2, 4}, {-0.42, 2, 3.5}}
	fore := []fl{{-0.31, 6, 6.5}, {-0.20, 10, 4.5}, {0.22, 8, 5.5}, {0.33, 4, 6.5}, {0.42, 10, 4}}
	set := back
	if front {
		set = fore
	}
	for i, f := range set {
		x, y := cx+f.dx*w, base+f.dy
		// stem and leaf
		pt.dc.SetColor(withAlpha(pt.t.gold, 0.35))
		if pt.t.dark {
			pt.dc.SetColor(withAlpha(hex("#4E8F7A"), 0.6))
		} else {
			pt.dc.SetColor(withAlpha(hex("#7FA79A"), 0.55))
		}
		pt.dc.SetLineWidth(0.9 * scale)
		pt.dc.DrawLine(x*scale, (y+f.r*0.6)*scale, (x+f.dx*6)*scale, (y+f.r*2.4)*scale)
		pt.dc.Stroke()
		c := pt.t.flower
		if i%2 == 1 {
			c = pt.t.flowerLight
		}
		cornflower(pt.dc, x, y, f.r, c, pt.t.dark)
	}
}

func cornflower(dc *gg.Context, cx, cy, r float64, c color.Color, glow bool) {
	if glow {
		g := gg.NewRadialGradient(cx*scale, cy*scale, 0, cx*scale, cy*scale, r*2.6*scale)
		g.AddColorStop(0, withAlpha(c, 0.45))
		g.AddColorStop(1, withAlpha(c, 0))
		dc.SetFillStyle(g)
		dc.DrawCircle(cx*scale, cy*scale, r*2.6*scale)
		dc.Fill()
	}
	dc.SetColor(c)
	for i := 0; i < 5; i++ {
		a := float64(i)*2*math.Pi/5 - math.Pi/2
		dc.Push()
		dc.Translate((cx+math.Cos(a)*r*0.55)*scale, (cy+math.Sin(a)*r*0.55)*scale)
		dc.Rotate(a)
		dc.DrawEllipse(0, 0, r*0.55*scale, r*0.36*scale)
		dc.Fill()
		dc.Pop()
	}
	dc.SetColor(hex("#FFFFFF"))
	dc.DrawCircle(cx*scale, cy*scale, r*0.22*scale)
	dc.Fill()
}

// callout shows the single tightest quota under the portrait.
func (pt *painter) callout(l *low, x, y, w float64) {
	dc, f, t := pt.dc, pt.f, pt.t
	h := calloutH - 14
	dc.SetColor(withAlpha(t.card, 0.75))
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 10*scale)
	dc.Fill()
	dc.SetColor(withAlpha(t.gold, 0.6))
	dc.SetLineWidth(0.8 * scale)
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 10*scale)
	dc.Stroke()

	dc.SetFontFace(f.caps)
	dc.SetColor(t.gold)
	drawSpaced(dc, "余量最少", x+14, y+21, 3, 0)
	_, c := levelColors(t, l.win.UsedPercent)
	dc.SetFontFace(f.big)
	dc.SetColor(c)
	dc.DrawStringAnchored(fmt.Sprintf("%.0f%%", 100-l.win.UsedPercent), (x+w-14)*scale, (y+32)*scale, 1, 0.5)
	dc.SetFontFace(f.caps)
	dc.SetColor(t.muted)
	drawSpaced(dc, "剩余", x+w-14, y+61, 2, 1)
	dc.SetFontFace(f.label)
	dc.SetColor(t.navy)
	dc.DrawString(truncate(dc, l.provider, (w-90)*scale), (x+14)*scale, (y+40)*scale)
	dc.SetFontFace(f.small)
	dc.SetColor(t.muted)
	line := zh(l.win.Label)
	if l.win.ResetsAt != nil {
		line += " · " + duration(l.win.ResetsAt.Sub(pt.now)) + "后重置"
	}
	dc.DrawString(truncate(dc, line, (w-28)*scale), (x+14)*scale, (y+60)*scale)
}

func (pt *painter) card(r provider.Report, x, y, w, h float64) {
	dc, f, t := pt.dc, pt.f, pt.t
	accent := t.accent(r.Provider)

	dc.SetColor(withAlpha(hex("#0A1640"), t.shadowAlpha))
	dc.DrawRoundedRectangle(x*scale, (y+3)*scale, w*scale, h*scale, 12*scale)
	dc.Fill()
	cardFill := t.card
	if t.dark {
		cardFill = withAlpha(t.card, 0.88)
	}
	dc.SetColor(cardFill)
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 12*scale)
	dc.Fill()
	dc.SetColor(t.border)
	dc.SetLineWidth(1 * scale)
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 12*scale)
	dc.Stroke()
	// Brand-coloured spine on the left edge.
	dc.Push()
	dc.DrawRoundedRectangle(x*scale, y*scale, w*scale, h*scale, 12*scale)
	dc.Clip()
	dc.SetColor(accent)
	dc.DrawRectangle(x*scale, y*scale, 4*scale, h*scale)
	dc.Fill()
	dc.ResetClip()
	dc.Pop()

	inner := x + cardPad
	innerW := w - 2*cardPad
	cy := y + cardPad + 10

	// Brand dot, then the provider name in the sheet's serif capitals.
	dc.SetColor(accent)
	dc.DrawCircle((inner+4)*scale, cy*scale, 4*scale)
	dc.Fill()
	dc.SetFontFace(f.name)
	dc.SetColor(t.navy)
	nameW := drawSpaced(dc, strings.ToUpper(r.Name), inner+16, cy+6, 0.6, 0)
	bx := inner + 16 + nameW + 12
	if r.Plan != "" {
		bx = pt.badge(r.Plan, bx, cy, t.royal, true) + 8
	}
	if r.Status != "ok" {
		label, c := "未配置", t.muted
		if r.Status == "error" {
			label, c = "出错", t.rose
		}
		pt.badge(label, bx, cy, c, false)
	}
	if r.Account != "" {
		dc.SetFontFace(f.small)
		dc.SetColor(t.muted)
		dc.DrawStringAnchored(r.Account, (inner+innerW)*scale, cy*scale, 1, 0.35)
	}

	ry := y + cardHeadH
	if r.Status != "ok" {
		dc.SetFontFace(f.small)
		dc.SetColor(t.muted)
		dc.DrawString(truncate(dc, r.Error, innerW*scale), inner*scale, (ry+12)*scale)
		return
	}
	if len(r.Windows) == 0 && len(r.Details) == 0 {
		dc.SetFontFace(f.small)
		dc.SetColor(t.muted)
		dc.DrawString("没有返回用量数据。", inner*scale, (ry+12)*scale)
		return
	}
	for _, win := range r.Windows {
		pt.window(win, inner, ry, innerW)
		ry += windowRowH
	}
	for i, d := range r.Details {
		if i == 0 {
			pt.rule(inner, ry-6, innerW)
		}
		dc.SetFontFace(f.label)
		dc.SetColor(t.muted)
		dc.DrawString(zh(d.Label), inner*scale, (ry+19)*scale)
		dc.SetFontFace(f.value)
		dc.SetColor(t.navy)
		drawBold(func(dx float64) { dc.DrawStringAnchored(zh(d.Value), (inner+innerW+dx)*scale, (ry+19)*scale, 1, 0) })
		ry += detailRowH
	}
}

func (pt *painter) window(win provider.Window, x, y, w float64) {
	dc, f, t := pt.dc, pt.f, pt.t
	from, to := levelColors(t, win.UsedPercent)

	dc.SetFontFace(f.label)
	dc.SetColor(t.text)
	dc.DrawString(zh(win.Label), x*scale, (y+14)*scale)
	dc.SetFontFace(f.value)
	dc.SetColor(to)
	used := fmt.Sprintf("已用 %.0f%%", win.UsedPercent)
	drawBold(func(dx float64) { dc.DrawStringAnchored(used, (x+w+dx)*scale, (y+14)*scale, 1, 0) })

	by, bh := y+24, 8.0
	dc.SetColor(t.track)
	dc.DrawRoundedRectangle(x*scale, by*scale, w*scale, bh*scale, bh/2*scale)
	dc.Fill()
	if fill := w * math.Min(win.UsedPercent, 100) / 100; fill > 0 {
		fill = max(fill, bh)
		g := gg.NewLinearGradient(x*scale, 0, (x+fill)*scale, 0)
		g.AddColorStop(0, from)
		g.AddColorStop(1, to)
		dc.SetFillStyle(g)
		dc.DrawRoundedRectangle(x*scale, by*scale, fill*scale, bh*scale, bh/2*scale)
		dc.Fill()
		// A pearl at the tip of the bar.
		dc.SetColor(hex("#FFFFFF"))
		dc.DrawCircle((x+fill-bh/2)*scale, (by+bh/2)*scale, 2.2*scale)
		dc.Fill()
	}
	if frac, ok := elapsed(win, pt.now); ok {
		pt.paceMark(x+w*frac, by-4, bh+8)
	}

	left := []string{remaining(win)}
	if a := amount(win); a != "" {
		left = append(left, a)
	}
	dc.SetFontFace(f.small)
	dc.SetColor(t.muted)
	dc.DrawString(strings.Join(left, "  ·  "), x*scale, (y+49)*scale)
	if rt := resetText(win.ResetsAt, pt.now); rt != "" {
		dc.DrawStringAnchored(rt, (x+w)*scale, (y+49)*scale, 1, 0)
	}
}

// paceMark draws the "how far through this window are we" tick: a thin bar topped by a
// small diamond. Usage to the right of it is ahead of an even pace.
func (pt *painter) paceMark(x, y, h float64) {
	dc := pt.dc
	dc.SetColor(withAlpha(pt.t.pace, 0.7))
	dc.SetLineWidth(1.4 * scale)
	dc.DrawLine(x*scale, (y+2)*scale, x*scale, (y+h)*scale)
	dc.Stroke()
	dc.SetColor(pt.t.gold)
	diamond(dc, x, y, 2.8, true)
}

// elapsed returns how far through its window a quota is, for windows of known length.
func elapsed(win provider.Window, now time.Time) (float64, bool) {
	if win.ResetsAt == nil {
		return 0, false
	}
	end := *win.ResetsAt
	var start time.Time
	id := strings.ToLower(win.ID)
	switch {
	case strings.HasSuffix(id, "session"):
		start = end.Add(-5 * time.Hour)
	case strings.Contains(id, "weekly"):
		start = end.AddDate(0, 0, -7)
	case strings.Contains(id, "monthly"):
		start = end.AddDate(0, -1, 0)
	default:
		return 0, false
	}
	frac := float64(now.Sub(start)) / float64(end.Sub(start))
	if frac <= 0 || frac >= 1 {
		return 0, false
	}
	return frac, true
}

// badge draws a pill and returns its right edge in logical pixels.
func (pt *painter) badge(text string, x, cy float64, c color.Color, filled bool) float64 {
	dc, t := pt.dc, pt.t
	dc.SetFontFace(pt.f.badge)
	tw, _ := dc.MeasureString(text)
	w, h := tw/scale+16, 20.0
	if filled {
		dc.SetColor(withAlpha(t.sky, 0.45))
	} else {
		dc.SetColor(withAlpha(c, 0.08))
	}
	dc.DrawRoundedRectangle(x*scale, (cy-h/2)*scale, w*scale, h*scale, h/2*scale)
	dc.Fill()
	if !filled {
		dc.SetColor(withAlpha(c, 0.6))
		dc.SetLineWidth(1 * scale)
		dc.DrawRoundedRectangle(x*scale, (cy-h/2)*scale, w*scale, h*scale, h/2*scale)
		dc.Stroke()
	}
	dc.SetColor(c)
	dc.DrawStringAnchored(text, (x+w/2)*scale, cy*scale, 0.5, 0.35)
	return x + w
}

// rule draws a hairline that fades out to the right, starting at a small star.
func (pt *painter) rule(x, y, w float64) {
	dc := pt.dc
	g := gg.NewLinearGradient(x*scale, 0, (x+w)*scale, 0)
	g.AddColorStop(0, withAlpha(pt.t.hairline, 0.9))
	g.AddColorStop(1, withAlpha(pt.t.hairline, 0))
	dc.SetStrokeStyle(g)
	dc.SetLineWidth(0.8 * scale)
	dc.DrawLine((x+10)*scale, y*scale, (x+w)*scale, y*scale)
	dc.Stroke()
	dc.SetColor(pt.t.cobalt)
	star(dc, x+3, y, 4)
}

// hemBorder echoes the gold embroidery on the hem of her dress: two thin gold lines
// with a chain of diamonds and small crosses between them, fading out to the right.
func (pt *painter) hemBorder(x, y, w float64) {
	dc, t := pt.dc, pt.t
	fade := func(a float64) gg.Pattern {
		g := gg.NewLinearGradient(x*scale, 0, (x+w)*scale, 0)
		g.AddColorStop(0, withAlpha(t.gold, a))
		g.AddColorStop(0.7, withAlpha(t.gold, a*0.6))
		g.AddColorStop(1, withAlpha(t.gold, 0))
		return g
	}
	dc.SetStrokeStyle(fade(0.85))
	dc.SetLineWidth(0.8 * scale)
	dc.DrawLine(x*scale, (y-7)*scale, (x+w)*scale, (y-7)*scale)
	dc.DrawLine(x*scale, (y+7)*scale, (x+w)*scale, (y+7)*scale)
	dc.Stroke()
	step := 34.0
	for i := 0; x+float64(i)*step < x+w-10; i++ {
		px := x + 10 + float64(i)*step
		a := math.Max(0, 1-float64(i)*step/w*1.1)
		if a <= 0.05 {
			break
		}
		dc.SetColor(withAlpha(t.gold, a))
		dc.SetLineWidth(0.8 * scale)
		diamond(dc, px, y, 4, false)
		dc.SetColor(withAlpha(t.cobalt, a))
		diamond(dc, px, y, 1.6, true)
		// small cross between diamonds
		dc.SetColor(withAlpha(t.gold, a*0.8))
		star(dc, px+step/2, y, 3)
	}
}

// drawSpaced draws letter-spaced text with its baseline at y, anchored horizontally by ax
// (0 left, 0.5 centre, 1 right), and returns its width in logical pixels.
func drawSpaced(dc *gg.Context, s string, x, y, spacing, ax float64) float64 {
	runes := []rune(s)
	widths := make([]float64, len(runes))
	total := 0.0
	for i, r := range runes {
		widths[i], _ = dc.MeasureString(string(r))
		total += widths[i]
	}
	total += spacing * scale * float64(max(len(runes)-1, 0))
	cx := x*scale - total*ax
	for i, r := range runes {
		dc.DrawString(string(r), cx, y*scale)
		cx += widths[i] + spacing*scale
	}
	return total / scale
}

// star fills a four-pointed sparkle, the motif on the character sheet's frame.
func star(dc *gg.Context, cx, cy, r float64) {
	in := r * 0.22
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		d := r
		if i%2 == 1 {
			d = in
		}
		dc.LineTo((cx+d*math.Sin(a))*scale, (cy-d*math.Cos(a))*scale)
	}
	dc.ClosePath()
	dc.Fill()
}

func diamond(dc *gg.Context, cx, cy, r float64, filled bool) {
	dc.MoveTo(cx*scale, (cy-r)*scale)
	dc.LineTo((cx+r)*scale, cy*scale)
	dc.LineTo(cx*scale, (cy+r)*scale)
	dc.LineTo((cx-r)*scale, cy*scale)
	dc.ClosePath()
	if filled {
		dc.Fill()
	} else {
		dc.Stroke()
	}
}

// levelColors returns the bar gradient for a usage level: cobalt, then gold, then rose.
func levelColors(t palette, pct float64) (from, to color.Color) {
	switch {
	case pct >= 90:
		if t.dark {
			return hex("#8A3A5A"), t.rose
		}
		return hex("#F2A7B9"), t.rose
	case pct >= 70:
		if t.dark {
			return hex("#7A6236"), t.gold
		}
		return hex("#E8CFA0"), t.gold
	default:
		return t.sky, t.cobalt
	}
}

func summary(out Output) string {
	ok, missing, failed := 0, 0, 0
	for _, r := range out.Providers {
		switch r.Status {
		case "ok":
			ok++
		case "unconfigured":
			missing++
		default:
			failed++
		}
	}
	parts := []string{fmt.Sprintf("已连接 %d 个", ok)}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("未配置 %d 个", missing))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("出错 %d 个", failed))
	}
	return strings.Join(parts, "，")
}

func truncate(dc *gg.Context, s string, maxW float64) string {
	if w, _ := dc.MeasureString(s); w <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if w, _ := dc.MeasureString(string(runes) + "…"); w <= maxW {
			break
		}
	}
	return string(runes) + "…"
}

func hex(s string) color.Color {
	var r, g, b uint8
	fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

func withAlpha(c color.Color, a float64) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a * 255)}
}

func stamp(out Output, now time.Time) time.Time {
	if out.GeneratedAt.IsZero() {
		return now
	}
	return out.GeneratedAt.Local()
}

// drawBold fakes a bold weight for the regular-only CJK fonts by drawing twice, the second
// time nudged right by a fraction of a pixel.
func drawBold(draw func(dx float64)) {
	draw(0)
	draw(0.45)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7f {
			return false
		}
	}
	return true
}
