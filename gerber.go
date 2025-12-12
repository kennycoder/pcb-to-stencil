package main

import (
	"bufio"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Aperture types
const (
	ApertureCircle  = "C"
	ApertureRect    = "R"
	ApertureObround = "O"
	// Add macros later if needed
)

type Aperture struct {
	Type      string
	Modifiers []float64
}

type MacroPrimitive struct {
	Code      int
	Modifiers []float64
}

type Macro struct {
	Name       string
	Primitives []MacroPrimitive
}

type GerberState struct {
	Apertures        map[int]Aperture
	Macros           map[string]Macro
	CurrentAperture  int
	X, Y             float64 // Current coordinates in mm
	FormatX, FormatY struct {
		Integer, Decimal int
	}
	Units string // "MM" or "IN"
}

type GerberCommand struct {
	Type string // "D01", "D02", "D03", "AD", "FS", etc.
	X, Y *float64
	D    *int
}

type GerberFile struct {
	Commands []GerberCommand
	State    GerberState
}

func NewGerberFile() *GerberFile {
	return &GerberFile{
		State: GerberState{
			Apertures: make(map[int]Aperture),
			Macros:    make(map[string]Macro),
			Units:     "MM", // Default, usually set by MO
		},
	}
}

// ParseGerber parses a simple RS-274X file
func ParseGerber(filename string) (*GerberFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gf := NewGerberFile()
	scanner := bufio.NewScanner(file)

	// Regex for coordinates: X123Y456D01
	reCoord := regexp.MustCompile(`([XYD])([\d\.\-]+)`)
	// Regex for Aperture Definition: %ADD10C,0.5*%
	reAD := regexp.MustCompile(`%ADD(\d+)([A-Za-z0-9_]+),?([\d\.X]+)?\*%`)
	// Regex for Format Spec: %FSLAX24Y24*%
	reFS := regexp.MustCompile(`%FSLAX(\d)(\d)Y(\d)(\d)\*%`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Handle Parameters
		if strings.HasPrefix(line, "%") {
			if strings.HasPrefix(line, "%FS") {
				matches := reFS.FindStringSubmatch(line)
				if len(matches) == 5 {
					gf.State.FormatX.Integer, _ = strconv.Atoi(matches[1])
					gf.State.FormatX.Decimal, _ = strconv.Atoi(matches[2])
					gf.State.FormatY.Integer, _ = strconv.Atoi(matches[3])
					gf.State.FormatY.Decimal, _ = strconv.Atoi(matches[4])
				}
			} else if strings.HasPrefix(line, "%AD") {
				matches := reAD.FindStringSubmatch(line)
				if len(matches) >= 3 {
					dCode, _ := strconv.Atoi(matches[1])
					apType := matches[2]
					var mods []float64
					if len(matches) > 3 && matches[3] != "" {
						parts := strings.Split(matches[3], "X")
						for _, p := range parts {
							val, _ := strconv.ParseFloat(p, 64)
							mods = append(mods, val)
						}
					}
					gf.State.Apertures[dCode] = Aperture{Type: apType, Modifiers: mods}
				}
			} else if strings.HasPrefix(line, "%AM") {
				// Parse Macro
				name := strings.TrimPrefix(line, "%AM")
				name = strings.TrimSuffix(name, "*")

				var primitives []MacroPrimitive

				for scanner.Scan() {
					mLine := strings.TrimSpace(scanner.Text())
					if mLine == "%" {
						break
					}
					mLine = strings.TrimSuffix(mLine, "*")
					parts := strings.Split(mLine, ",")
					if len(parts) > 0 {
						code, _ := strconv.Atoi(parts[0])
						var mods []float64
						for _, p := range parts[1:] {
							val, _ := strconv.ParseFloat(p, 64)
							mods = append(mods, val)
						}
						primitives = append(primitives, MacroPrimitive{Code: code, Modifiers: mods})
					}
				}
				gf.State.Macros[name] = Macro{Name: name, Primitives: primitives}
			} else if strings.HasPrefix(line, "%MO") {
				if strings.Contains(line, "IN") {
					gf.State.Units = "IN"
				} else {
					gf.State.Units = "MM"
				}
			}
			continue
		}

		// Handle Standard Commands
		// Split by *
		parts := strings.Split(line, "*")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			// Check for G-codes (G54 is aperture selection in older files, but usually Dnn is used)
			// We focus on D-codes and Coordinates

			// Handle Aperture Selection (e.g., D10*)
			if strings.HasPrefix(part, "D") && len(part) >= 2 {
				// Likely D10, D11 etc.
				dCode, err := strconv.Atoi(part[1:])
				if err == nil && dCode >= 10 {
					gf.Commands = append(gf.Commands, GerberCommand{Type: "APERTURE", D: &dCode})
					continue
				}
			}

			// Handle Coordinates and Draw/Flash commands
			// X...Y...D01*
			matches := reCoord.FindAllStringSubmatch(part, -1)
			if len(matches) > 0 {
				cmd := GerberCommand{Type: "MOVE"}
				for _, m := range matches {
					valStr := m[2]

					switch m[1] {
					case "X":
						v := gf.parseCoordinate(valStr, gf.State.FormatX)
						cmd.X = &v
					case "Y":
						v := gf.parseCoordinate(valStr, gf.State.FormatY)
						cmd.Y = &v
					case "D":
						val, _ := strconv.ParseFloat(valStr, 64)
						d := int(val)
						cmd.D = &d
						if d == 1 {
							cmd.Type = "DRAW"
						} else if d == 2 {
							cmd.Type = "MOVE"
						} else if d == 3 {
							cmd.Type = "FLASH"
						}
					}
				}
				gf.Commands = append(gf.Commands, cmd)
			}
		}
	}

	return gf, nil
}

func (gf *GerberFile) parseCoordinate(valStr string, fmtSpec struct{ Integer, Decimal int }) float64 {
	if strings.Contains(valStr, ".") {
		val, _ := strconv.ParseFloat(valStr, 64)
		return val
	}
	val, _ := strconv.ParseFloat(valStr, 64)
	divisor := math.Pow(10, float64(fmtSpec.Decimal))
	return val / divisor
}

type Bounds struct {
	MinX, MinY, MaxX, MaxY float64
}

func (gf *GerberFile) CalculateBounds() Bounds {
	minX, minY := 1e9, 1e9
	maxX, maxY := -1e9, -1e9

	updateBounds := func(x, y float64) {
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x > maxX {
			maxX = x
		}
		if y > maxY {
			maxY = y
		}
	}

	curX, curY := 0.0, 0.0
	for _, cmd := range gf.Commands {
		prevX, prevY := curX, curY
		if cmd.X != nil {
			curX = *cmd.X
		}
		if cmd.Y != nil {
			curY = *cmd.Y
		}

		if cmd.Type == "FLASH" {
			updateBounds(curX, curY)
		} else if cmd.Type == "DRAW" {
			updateBounds(prevX, prevY)
			updateBounds(curX, curY)
		}
	}

	if minX == 1e9 {
		// No drawing commands found, default to 0,0
		minX, minY = 0, 0
		maxX, maxY = 10, 10 // Arbitrary small size
	}

	// Add some padding
	padding := 2.0 // mm
	minX -= padding
	minY -= padding
	maxX += padding
	maxY += padding

	return Bounds{MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY}
}

// Render generates an image from the parsed Gerber commands
func (gf *GerberFile) Render(dpi float64, bounds *Bounds) image.Image {
	var b Bounds
	if bounds != nil {
		b = *bounds
	} else {
		b = gf.CalculateBounds()
	}

	widthMM := b.MaxX - b.MinX
	heightMM := b.MaxY - b.MinY

	var scale float64
	if gf.State.Units == "IN" {
		scale = dpi
	} else {
		scale = dpi / 25.4
	}

	imgWidth := int(widthMM * scale)
	imgHeight := int(heightMM * scale)

	img := image.NewRGBA(image.Rect(0, 0, imgWidth, imgHeight))

	// Fill black (stencil material)
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	// White for holes
	white := &image.Uniform{color.White}

	// Helper to convert mm to pixels
	toPix := func(x, y float64) (int, int) {
		px := int((x - b.MinX) * scale)
		py := int((heightMM - (y - b.MinY)) * scale) // Flip Y for image coords
		return px, py
	}

	curX, curY := 0.0, 0.0
	curDCode := 0

	for _, cmd := range gf.Commands {
		if cmd.Type == "APERTURE" {
			curDCode = *cmd.D
			continue
		}

		prevX, prevY := curX, curY
		if cmd.X != nil {
			curX = *cmd.X
		}
		if cmd.Y != nil {
			curY = *cmd.Y
		}

		if cmd.Type == "FLASH" {
			// Draw Aperture at curX, curY
			ap, ok := gf.State.Apertures[curDCode]
			if ok {
				cx, cy := toPix(curX, curY)
				gf.drawAperture(img, cx, cy, ap, scale, white)
			}
		} else if cmd.Type == "DRAW" {
			// Draw Line from prevX, prevY to curX, curY using current aperture
			ap, ok := gf.State.Apertures[curDCode]
			if ok {
				x1, y1 := toPix(prevX, prevY)
				x2, y2 := toPix(curX, curY)
				gf.drawLine(img, x1, y1, x2, y2, ap, scale, white)
			}
		}
	}

	return img
}

func (gf *GerberFile) drawAperture(img *image.RGBA, x, y int, ap Aperture, scale float64, c image.Image) {
	switch ap.Type {
	case ApertureCircle: // C
		// Modifiers[0] is diameter
		if len(ap.Modifiers) > 0 {
			radius := int((ap.Modifiers[0] * scale) / 2)
			drawCircle(img, x, y, radius)
		}
		return
	case ApertureRect: // R
		// Modifiers[0] is width, [1] is height
		if len(ap.Modifiers) >= 2 {
			w := int(ap.Modifiers[0] * scale)
			h := int(ap.Modifiers[1] * scale)
			r := image.Rect(x-w/2, y-h/2, x+w/2, y+h/2)
			draw.Draw(img, r, c, image.Point{}, draw.Src)
		}
		return
	case ApertureObround: // O
		// Similar to rect but with rounded corners. For now, treat as Rect or implement properly.
		// Implementing as Rect for MVP
		if len(ap.Modifiers) >= 2 {
			w := int(ap.Modifiers[0] * scale)
			h := int(ap.Modifiers[1] * scale)
			r := image.Rect(x-w/2, y-h/2, x+w/2, y+h/2)
			draw.Draw(img, r, c, image.Point{}, draw.Src)
		}
		return
	}

	// Check for Macros
	if macro, ok := gf.State.Macros[ap.Type]; ok {
		for _, prim := range macro.Primitives {
			switch prim.Code {
			case 1: // Circle
				// Mods: Exposure, Diameter, CenterX, CenterY
				if len(prim.Modifiers) >= 4 {
					// exposure := prim.Modifiers[0] // 1=on, 0=off (assuming 1 for now)
					dia := prim.Modifiers[1]
					cx := prim.Modifiers[2]
					cy := prim.Modifiers[3]

					px := int(cx * scale)
					py := int(cy * scale)

					radius := int((dia * scale) / 2)
					drawCircle(img, x+px, y-py, radius)
				}
			case 21: // Center Line (Rect)
				// Mods: Exposure, Width, Height, CenterX, CenterY, Rotation
				if len(prim.Modifiers) >= 6 {
					width := prim.Modifiers[1]
					height := prim.Modifiers[2]
					cx := prim.Modifiers[3]
					cy := prim.Modifiers[4]
					rot := prim.Modifiers[5]

					// Normalize rotation to 0-360
					rot = math.Mod(rot, 360)
					if rot < 0 {
						rot += 360
					}

					// Handle simple 90-degree rotations (swap width/height)
					if math.Abs(rot-90) < 1.0 || math.Abs(rot-270) < 1.0 {
						width, height = height, width
					}

					w := int(width * scale)
					h := int(height * scale)
					icx := int(cx * scale)
					icy := int(cy * scale)

					rx := x + icx
					ry := y - icy

					r := image.Rect(rx-w/2, ry-h/2, rx+w/2, ry+h/2)
					draw.Draw(img, r, c, image.Point{}, draw.Src)
				}
			}
		}
	}
}

func drawCircle(img *image.RGBA, x0, y0, r int) {
	// Simple Bresenham or scanline
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				img.Set(x0+x, y0+y, color.White)
			}
		}
	}
}

func (gf *GerberFile) drawLine(img *image.RGBA, x1, y1, x2, y2 int, ap Aperture, scale float64, c image.Image) {
	// Bresenham's line algorithm, but we need to stroke it with the aperture.
	// For simplicity, if aperture is Circle, we draw a circle at each step (inefficient but works).
	// If aperture is Rect, we draw rect at each step.

	// Optimized: Just draw a thick line if it's a circle aperture

	dx := float64(x2 - x1)
	dy := float64(y2 - y1)
	dist := math.Sqrt(dx*dx + dy*dy)
	steps := int(dist) // 1 pixel steps

	if steps == 0 {
		gf.drawAperture(img, x1, y1, ap, scale, c)
		return
	}

	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(float64(x1) + t*dx)
		y := int(float64(y1) + t*dy)
		gf.drawAperture(img, x, y, ap, scale, c)
	}
}
