package canvas

import (
	"exp/draw"
	"image"
	"math"
	"freetype-go.googlecode.com/hg/freetype/raster"
)

// Box creates a rectangular image of the given size, filled with the given colour,
// with a border-size border of colour borderCol.
//
func Box(width, height int, col image.Image, border int, borderCol image.Image) image.Image {
	img := image.NewRGBA(width, height)
	if border < 0 {
		border = 0
	}
	r := draw.Rect(0, 0, width, height)
	draw.DrawMask(img, r.Inset(border), col, draw.ZP, nil, draw.ZP, draw.Src)
	BorderOp(img, r, border, borderCol, draw.ZP, draw.Src)
	return img
}

// An ImageItem is an Item that uses an image
// to draw itself. It is intended to be used as a building
// block for other Items.
type ImageItem struct {
	r      draw.Rectangle
	img    image.Image
	opaque bool
}

func (obj *ImageItem) Draw(dst *image.RGBA, clip draw.Rectangle) {
	dr := obj.r.Clip(clip)
	sp := dr.Min.Sub(obj.r.Min)
	op := draw.Over
	if obj.opaque {
		op = draw.Src
	}
	draw.DrawMask(dst, dr, obj.img, sp, nil, draw.ZP, op)
}

func (obj *ImageItem) SetContainer(c Backing) {
}

func (obj *ImageItem) Opaque() bool {
	return obj.opaque
}

func (obj *ImageItem) Bbox() draw.Rectangle {
	return obj.r
}

func (obj *ImageItem) HitTest(p draw.Point) bool {
	return p.In(obj.r)
}

// An Image represents an rectangular (but possibly
// transparent) image.
//
type Image struct {
	Item
	item   ImageItem // access to the fields of the ImageItem
	canvas Backing
}

// Image returns a new Image which will be drawn using img,
// with p giving the coordinate of the image's top left corner.
//
func NewImage(img image.Image, opaque bool, p draw.Point) *Image {
	obj := new(Image)
	obj.Item = &obj.item
	obj.item.r = draw.Rectangle{p, p.Add(draw.Pt(img.Width(), img.Height()))}
	obj.item.img = img
	obj.item.opaque = opaque
	return obj
}

func (obj *Image) SetContainer(c Backing) {
	obj.canvas = c
}

// SetMinPoint moves the image's upper left corner to p.
//
func (obj *Image) SetMinPoint(p draw.Point) {
	if p.Eq(obj.item.r.Min) {
		return
	}
	obj.canvas.Atomically(func(flush FlushFunc) {
		r := obj.item.r
		obj.item.r = r.Add(p.Sub(r.Min))
		flush(r, nil)
		flush(obj.item.r, nil)
	})
}

func (obj *Image) SetCentre(p draw.Point) {
	obj.SetMinPoint(p.Sub(centreDist(obj.Bbox())))
}

// A Polygon represents a filled polygon.
//
type Polygon struct {
	Item
	raster RasterItem
	canvas Backing
	points []raster.Point
}

// Polygon returns a new PolyObject, using col for its fill colour, and
// using points for its vertices.
//
func NewPolygon(col image.Color, points []draw.Point) *Polygon {
	obj := new(Polygon)
	rpoints := make([]raster.Point, len(points))
	for i, p := range points {
		rpoints[i] = pixel2fixPoint(p)
	}
	obj.raster.SetColor(col)
	obj.points = rpoints
	obj.Item = &obj.raster
	return obj
}

func (obj *Polygon) SetContainer(c Backing) {
	obj.canvas = c
	if c != nil {
		obj.raster.SetBounds(c.Width(), c.Height())
		obj.rasterize()
	}
}

func (obj *Polygon) SetCentre(cp draw.Point) {
	obj.canvas.Atomically(func(flush FlushFunc) {
		r := obj.raster.Bbox()
		delta := cp.Sub(centre(r))
		rdelta := pixel2fixPoint(delta)
		for i := range obj.points {
			p := &obj.points[i]
			p.X += rdelta.X
			p.Y += rdelta.Y
		}
		obj.rasterize()
		flush(r, nil)
		flush(obj.raster.Bbox(), nil)
	})
}

func (obj *Polygon) rasterize() {
	obj.raster.Clear()
	if len(obj.points) > 0 {
		obj.raster.Start(obj.points[0])
		for _, p := range obj.points[1:] {
			obj.raster.Add1(p)
		}
		obj.raster.Add1(obj.points[0])
	}
	obj.raster.CalcBbox()
}

// A line object represents a single straight line.
type Line struct {
	Item
	raster RasterItem
	canvas Backing
	p0, p1 raster.Point
	width  raster.Fixed
}

// Line returns a new Line, coloured with col, from p0 to p1,
// of the given width.
//
func NewLine(col image.Color, p0, p1 draw.Point, width float) *Line {
	obj := new(Line)
	obj.p0 = pixel2fixPoint(p0)
	obj.p1 = pixel2fixPoint(p1)
	obj.width = float2fix(width)
	obj.raster.SetColor(col)
	obj.Item = &obj.raster
	obj.rasterize()
	return obj
}

func (obj *Line) SetContainer(c Backing) {
	obj.canvas = c
	if c != nil {
		obj.raster.SetBounds(c.Width(), c.Height())
		obj.rasterize()
	}
}

func (obj *Line) rasterize() {
	obj.raster.Clear()
	sin, cos := isincos2(obj.p1.X-obj.p0.X, obj.p1.Y-obj.p0.Y)
	dx := (cos * obj.width) / (2 * fixScale)
	dy := (sin * obj.width) / (2 * fixScale)
	q := raster.Point{
		obj.p0.X + fixScale/2 - sin/2,
		obj.p0.Y + fixScale/2 - cos/2,
	}
	p0 := raster.Point{q.X - dx, q.Y + dy}
	obj.raster.Start(p0)
	obj.raster.Add1(raster.Point{q.X + dx, q.Y - dy})

	q = raster.Point{
		obj.p1.X + fixScale/2 + sin/2,
		obj.p1.Y + fixScale/2 + cos/2,
	}
	obj.raster.Add1(raster.Point{q.X + dx, q.Y - dy})
	obj.raster.Add1(raster.Point{q.X - dx, q.Y + dy})
	obj.raster.Add1(p0)
	obj.raster.CalcBbox()
}
func (obj *Line) SetCentre(cp draw.Point) {
	delta := cp.Sub(centre(obj.Bbox()))
	p0 := fix2pixelPoint(obj.p0)
	p1 := fix2pixelPoint(obj.p1)
	obj.SetEndPoints(p0.Add(delta), p1.Add(delta))
}

// SetEndPoints changes the end coordinates of the Line.
//
func (obj *Line) SetEndPoints(p0, p1 draw.Point) {
	obj.canvas.Atomically(func(flush FlushFunc) {
		r := obj.raster.Bbox()
		obj.p0 = pixel2fixPoint(p0)
		obj.p1 = pixel2fixPoint(p1)
		obj.rasterize()
		flush(r, nil)
		flush(obj.raster.Bbox(), nil)
	})
}

// SetColor changes the colour of the line
//
func (obj *Line) SetColor(col image.Color) {
	obj.canvas.Atomically(func(flush FlushFunc) {
		obj.raster.SetColor(col)
		flush(obj.raster.Bbox(), nil)
	})
}

// could do it in fixed point, but what's 0.5us between friends?
func isincos2(x, y raster.Fixed) (isin, icos raster.Fixed) {
	sin, cos := math.Sincos(math.Atan2(fixed2float(x), fixed2float(y)))
	isin = float2fixed(sin)
	icos = float2fixed(cos)
	return
}

type Slider struct {
	backing Backing
	value Value
	Item
	c      *Canvas
	val    float64
	box    ImageItem
	button ImageItem
}

// A Slider shows a mouse-adjustable slider bar.
// NewSlider returns the Slider item.
// The value is used to set and get the current slider value;
// its Type() should be float64; the slider's value is in the
// range [0, 1].
//
func NewSlider(r draw.Rectangle, fg, bg image.Color, value Value) (obj *Slider) {
	obj = new(Slider)
	obj.value = value
	obj.c = NewCanvas(nil, nil, r)
	obj.box.r = r
	obj.box.img = Box(r.Dx(), r.Dy(), image.ColorImage{bg}, 1, image.Black)
	obj.box.opaque = opaqueColor(bg)

	br := obj.buttonRect()
	obj.button.r = br
	obj.button.img = Box(br.Dx(), br.Dy(), image.ColorImage{fg}, 1, image.Black)
	obj.button.opaque = opaqueColor(fg)
	obj.c.AddItem(&obj.box)
	obj.c.AddItem(&obj.button)

	go obj.listener()

	obj.Item = obj.c
	return obj
}

const buttonWidth = 6

func (obj *Slider) SetContainer(c Backing) {
	obj.backing = c
}

func (obj *Slider) buttonRect() (r draw.Rectangle) {
	r.Min.Y = obj.box.r.Min.Y
	r.Max.Y = obj.box.r.Max.Y
	p := obj.val
	centre := int(p*float64(obj.box.r.Max.X-obj.box.r.Min.X-buttonWidth)+0.5) + obj.box.r.Min.X + buttonWidth/2
	r.Min.X = centre - buttonWidth/2
	r.Max.X = centre + buttonWidth/2
	return
}

func (obj *Slider) listener() {
	for x := range obj.value.Iter() {
		v := x.(float64)
		obj.backing.Atomically(func(flush FlushFunc) {
			if v > 1 {
				v = 1
			}
			if v < 0 {
				v = 0
			}
			obj.val = v
			r := obj.button.r
			obj.button.r = obj.buttonRect()
			flush(r, nil)
			flush(obj.button.r, nil)
		})
		obj.backing.Flush()
	}
}

func (obj *Slider) x2val(x int) float64 {
	return float64(x-(obj.box.r.Min.X+buttonWidth/2)) / float64(obj.box.r.Dx()-buttonWidth)
}

func (obj *Slider) HandleMouse(f Flusher, m draw.Mouse, mc <-chan draw.Mouse) bool {
	if m.Buttons&1 == 0 {
		return false
	}
	offset := 0
	br := obj.buttonRect()
	if !m.In(br) {
		obj.value.Set(obj.x2val(m.X))
	} else {
		offset = m.X - (br.Min.X+br.Max.X)/2
	}

	but := m.Buttons
	for {
		m = <-mc
		obj.value.Set(obj.x2val(m.X - offset))
		if (m.Buttons & but) != but {
			break
		}
	}
	return true
}


func opaqueColor(col image.Color) bool {
	_, _, _, a := col.RGBA()
	return a == 0xffff
}

func DrawOp(dst draw.Image, r draw.Rectangle, src image.Image, sp draw.Point, op draw.Op) {
	draw.DrawMask(dst, r, src, sp, nil, draw.ZP, op)
}

// Border aligns r.Min in dst with sp in src and then replaces pixels
// in a w-pixel border around r in dst with the result of the Porter-Duff compositing
// operation ``src over dst.''  If w is positive, the border extends w pixels inside r.
// If w is negative, the border extends w pixels outside r.
func BorderOp(dst draw.Image, r draw.Rectangle, w int, src image.Image, sp draw.Point, op draw.Op) {
	i := w
	if i > 0 {
		// inside r
		DrawOp(dst, draw.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+i), src, sp, op)                          // top
		DrawOp(dst, draw.Rect(r.Min.X, r.Min.Y+i, r.Min.X+i, r.Max.Y-i), src, sp.Add(draw.Pt(0, i)), op)        // left
		DrawOp(dst, draw.Rect(r.Max.X-i, r.Min.Y+i, r.Max.X, r.Max.Y-i), src, sp.Add(draw.Pt(r.Dx()-i, i)), op) // right
		DrawOp(dst, draw.Rect(r.Min.X, r.Max.Y-i, r.Max.X, r.Max.Y), src, sp.Add(draw.Pt(0, r.Dy()-i)), op)     // bottom
		return
	}

	// outside r;
	i = -i
	DrawOp(dst, draw.Rect(r.Min.X-i, r.Min.Y-i, r.Max.X+i, r.Min.Y), src, sp.Add(draw.Pt(-i, -i)), op) // top
	DrawOp(dst, draw.Rect(r.Min.X-i, r.Min.Y, r.Min.X, r.Max.Y), src, sp.Add(draw.Pt(-i, 0)), op)      // left
	DrawOp(dst, draw.Rect(r.Max.X, r.Min.Y, r.Max.X+i, r.Max.Y), src, sp.Add(draw.Pt(r.Dx(), 0)), op)  // right
	DrawOp(dst, draw.Rect(r.Min.X-i, r.Max.Y, r.Max.X+i, r.Max.Y+i), src, sp.Add(draw.Pt(-i, 0)), op)  // bottom
}

func centreDist(r draw.Rectangle) draw.Point {
	return draw.Pt(r.Dx() / 2, r.Dy() / 2)
}

func centre(r draw.Rectangle) draw.Point {
	return draw.Pt((r.Min.X + r.Max.X) / 2, (r.Min.Y + r.Max.Y) / 2)
}
