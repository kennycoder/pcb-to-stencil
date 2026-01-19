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
	Modifiers []string // Store as strings to handle $1, $2, expressions like $1+$1
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
	I, J *float64
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
	reCoord := regexp.MustCompile(`([XYDIJ])([\d\.\-]+)`)
	// Regex for Aperture Definition: %ADD10C,0.5*% or %ADD10RoundRect,0.25X-0.75X...*%
	reAD := regexp.MustCompile(`%ADD(\d+)([A-Za-z0-9_]+),?([\d\.\-X]+)?\*%`)
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
					// Skip comment lines (start with "0 ")
					if strings.HasPrefix(mLine, "0 ") {
						continue
					}
					// Skip empty lines
					if mLine == "" {
						continue
					}
					
					// Check if this line ends the macro definition
					endsWithPercent := strings.HasSuffix(mLine, "*%")
					
					// Remove trailing *% or just *
					mLine = strings.TrimSuffix(mLine, "*%")
					mLine = strings.TrimSuffix(mLine, "*")
					
					// Parse primitive
					parts := strings.Split(mLine, ",")
					if len(parts) > 0 && parts[0] != "" {
						code, err := strconv.Atoi(parts[0])
						if err == nil && code > 0 {
							// Store modifiers as strings to handle $1, $2, expressions
							var mods []string
							for _, p := range parts[1:] {
								mods = append(mods, strings.TrimSpace(p))
							}
							primitives = append(primitives, MacroPrimitive{Code: code, Modifiers: mods})
						}
					}
					
					// If line ended with *%, macro definition is complete
					if endsWithPercent {
						break
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

			// Check for G-codes
			if strings.HasPrefix(part, "G") {
				if part == "G01" {
					// Linear interpolation (default)
					gf.Commands = append(gf.Commands, GerberCommand{Type: "G01"})
				} else if part == "G02" {
					// Clockwise circular interpolation
					gf.Commands = append(gf.Commands, GerberCommand{Type: "G02"})
				} else if part == "G03" {
					// Counter-clockwise circular interpolation
					gf.Commands = append(gf.Commands, GerberCommand{Type: "G03"})
				}
				continue
			}

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
					case "I":
						v := gf.parseCoordinate(valStr, gf.State.FormatX)
						cmd.I = &v
					case "J":
						v := gf.parseCoordinate(valStr, gf.State.FormatY)
						cmd.J = &v
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

// evaluateMacroExpression evaluates a macro expression like "$1", "$1+$1", "0.5", etc.
// with variable substitution from aperture modifiers
func evaluateMacroExpression(expr string, params []float64) float64 {
	expr = strings.TrimSpace(expr)
	
	// Handle simple addition (e.g., "$1+$1")
	if strings.Contains(expr, "+") {
		parts := strings.Split(expr, "+")
		result := 0.0
		for _, part := range parts {
			result += evaluateMacroExpression(strings.TrimSpace(part), params)
		}
		return result
	}
	
	// Handle simple subtraction (e.g., "$1-$2")
	if strings.Contains(expr, "-") && !strings.HasPrefix(expr, "-") {
		parts := strings.Split(expr, "-")
		if len(parts) == 2 {
			left := evaluateMacroExpression(strings.TrimSpace(parts[0]), params)
			right := evaluateMacroExpression(strings.TrimSpace(parts[1]), params)
			return left - right
		}
	}
	
	// Handle variable substitution (e.g., "$1", "$2")
	if strings.HasPrefix(expr, "$") {
		varNum, err := strconv.Atoi(expr[1:])
		if err == nil && varNum > 0 && varNum <= len(params) {
			return params[varNum-1]
		}
		return 0.0
	}
	
	// Handle literal numbers
	val, _ := strconv.ParseFloat(expr, 64)
	return val
}

// rotatePoint rotates a point (x, y) around the origin by angleDegrees
func rotatePoint(x, y, angleDegrees float64) (float64, float64) {
	if angleDegrees == 0 {
		return x, y
	}
	angleRad := angleDegrees * math.Pi / 180.0
	cosA := math.Cos(angleRad)
	sinA := math.Sin(angleRad)
	return x*cosA - y*sinA, x*sinA + y*cosA
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
	interpolationMode := "G01" // Default linear

	for _, cmd := range gf.Commands {
		if cmd.Type == "APERTURE" {
			curDCode = *cmd.D
			continue
		}
		if cmd.Type == "G01" || cmd.Type == "G02" || cmd.Type == "G03" {
			interpolationMode = cmd.Type
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
			ap, ok := gf.State.Apertures[curDCode]
			if ok {
				if interpolationMode == "G01" {
					// Linear
					x1, y1 := toPix(prevX, prevY)
					x2, y2 := toPix(curX, curY)
					gf.drawLine(img, x1, y1, x2, y2, ap, scale, white)
				} else {
					// Circular Interpolation (G02/G03)
					// I and J are offsets from start point (prevX, prevY) to center
					iVal := 0.0
					jVal := 0.0
					if cmd.I != nil {
						iVal = *cmd.I
					}
					if cmd.J != nil {
						jVal = *cmd.J
					}

					centerX := prevX + iVal
					centerY := prevY + jVal

					radius := math.Sqrt(iVal*iVal + jVal*jVal)
					startAngle := math.Atan2(prevY-centerY, prevX-centerX)
					endAngle := math.Atan2(curY-centerY, curX-centerX)

					// Adjust angles for G02 (CW) vs G03 (CCW)
					if interpolationMode == "G03" { // CCW
						if endAngle <= startAngle {
							endAngle += 2 * math.Pi
						}
					} else { // G02 CW
						if startAngle <= endAngle {
							startAngle += 2 * math.Pi
						}
					}

					// Arc length approximation
					arcLen := math.Abs(endAngle-startAngle) * radius
					steps := int(arcLen * scale * 2) // 2x pixel density for smoothness
					if steps < 10 {
						steps = 10
					}

					for s := 0; s <= steps; s++ {
						t := float64(s) / float64(steps)
						angle := startAngle + t*(endAngle-startAngle)
						px := centerX + radius*math.Cos(angle)
						py := centerY + radius*math.Sin(angle)

						ix, iy := toPix(px, py)
						gf.drawAperture(img, ix, iy, ap, scale, white)
					}
				}
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
					exposure := evaluateMacroExpression(prim.Modifiers[0], ap.Modifiers)
					if exposure == 0 {
						break
					}
					dia := evaluateMacroExpression(prim.Modifiers[1], ap.Modifiers)
					cx := evaluateMacroExpression(prim.Modifiers[2], ap.Modifiers)
					cy := evaluateMacroExpression(prim.Modifiers[3], ap.Modifiers)

					px := int(cx * scale)
					py := int(cy * scale)

					radius := int((dia * scale) / 2)
					drawCircle(img, x+px, y-py, radius)
				}
			case 4: // Outline (Polygon)
				// Mods: Exposure, NumVertices, X1, Y1, ..., Xn, Yn, Rotation
				if len(prim.Modifiers) >= 3 {
					exposure := evaluateMacroExpression(prim.Modifiers[0], ap.Modifiers)
					if exposure == 0 {
						// Skip if exposure is off
						break
					}
					numVertices := int(evaluateMacroExpression(prim.Modifiers[1], ap.Modifiers))
					// Need at least numVertices * 2 coordinates + rotation
					if len(prim.Modifiers) >= 2+numVertices*2+1 {
						rotation := evaluateMacroExpression(prim.Modifiers[2+numVertices*2], ap.Modifiers)
						
						// Extract vertices
						vertices := make([][2]int, numVertices)
						for i := 0; i < numVertices; i++ {
							vx := evaluateMacroExpression(prim.Modifiers[2+i*2], ap.Modifiers)
							vy := evaluateMacroExpression(prim.Modifiers[2+i*2+1], ap.Modifiers)
							
							// Apply rotation
							vx, vy = rotatePoint(vx, vy, rotation)
							
							px := int(vx * scale)
							py := int(vy * scale)
							
							vertices[i] = [2]int{x + px, y - py}
						}
						
						// Draw filled polygon using scanline algorithm
						drawFilledPolygon(img, vertices, c)
					}
				}
			case 20: // Vector Line
				// Mods: Exposure, Width, StartX, StartY, EndX, EndY, Rotation
				if len(prim.Modifiers) >= 7 {
					exposure := evaluateMacroExpression(prim.Modifiers[0], ap.Modifiers)
					if exposure == 0 {
						// Skip if exposure is off
						break
					}
					width := evaluateMacroExpression(prim.Modifiers[1], ap.Modifiers)
					startX := evaluateMacroExpression(prim.Modifiers[2], ap.Modifiers)
					startY := evaluateMacroExpression(prim.Modifiers[3], ap.Modifiers)
					endX := evaluateMacroExpression(prim.Modifiers[4], ap.Modifiers)
					endY := evaluateMacroExpression(prim.Modifiers[5], ap.Modifiers)
					rotation := evaluateMacroExpression(prim.Modifiers[6], ap.Modifiers)
					
					// Apply rotation to start and end points
					startX, startY = rotatePoint(startX, startY, rotation)
					endX, endY = rotatePoint(endX, endY, rotation)
					
					// Calculate the rectangle representing the line
					// The line goes from (startX, startY) to (endX, endY) with width
					dx := endX - startX
					dy := endY - startY
					length := math.Sqrt(dx*dx + dy*dy)
					
					if length > 0 {
						// Perpendicular vector for width
						perpX := -dy / length * width / 2
						perpY := dx / length * width / 2
						
						// Four corners of the rectangle
						vertices := [][2]int{
							{x + int((startX-perpX)*scale), y - int((startY-perpY)*scale)},
							{x + int((startX+perpX)*scale), y - int((startY+perpY)*scale)},
							{x + int((endX+perpX)*scale), y - int((endY+perpY)*scale)},
							{x + int((endX-perpX)*scale), y - int((endY-perpY)*scale)},
						}
						
						drawFilledPolygon(img, vertices, c)
					}
				}
			case 21: // Center Line (Rect)
				// Mods: Exposure, Width, Height, CenterX, CenterY, Rotation
				if len(prim.Modifiers) >= 6 {
					exposure := evaluateMacroExpression(prim.Modifiers[0], ap.Modifiers)
					if exposure == 0 {
						break
					}
					width := evaluateMacroExpression(prim.Modifiers[1], ap.Modifiers)
					height := evaluateMacroExpression(prim.Modifiers[2], ap.Modifiers)
					cx := evaluateMacroExpression(prim.Modifiers[3], ap.Modifiers)
					cy := evaluateMacroExpression(prim.Modifiers[4], ap.Modifiers)
					rot := evaluateMacroExpression(prim.Modifiers[5], ap.Modifiers)

					// Calculate the four corners of the rectangle (centered at origin)
					halfW := width / 2
					halfH := height / 2
					
					// Four corners before rotation
					corners := [][2]float64{
						{-halfW, -halfH},
						{halfW, -halfH},
						{halfW, halfH},
						{-halfW, halfH},
					}
					
					// Apply rotation and translation
					vertices := make([][2]int, 4)
					for i, corner := range corners {
						// Rotate around origin
						rx, ry := rotatePoint(corner[0], corner[1], rot)
						// Translate to center position
						rx += cx
						ry += cy
						// Convert to pixels
						px := int(rx * scale)
						py := int(ry * scale)
						vertices[i] = [2]int{x + px, y - py}
					}
					
					// Draw as polygon
					drawFilledPolygon(img, vertices, c)
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

func drawFilledPolygon(img *image.RGBA, vertices [][2]int, c image.Image) {
	if len(vertices) < 3 {
		return
	}

	// Find bounding box
	minY, maxY := vertices[0][1], vertices[0][1]
	for _, v := range vertices {
		if v[1] < minY {
			minY = v[1]
		}
		if v[1] > maxY {
			maxY = v[1]
		}
	}

	// Scanline fill algorithm
	for y := minY; y <= maxY; y++ {
		// Find intersections with polygon edges
		var intersections []int

		for i := 0; i < len(vertices); i++ {
			j := (i + 1) % len(vertices)
			y1, y2 := vertices[i][1], vertices[j][1]
			x1, x2 := vertices[i][0], vertices[j][0]

			// Check if scanline intersects this edge
			if (y1 <= y && y < y2) || (y2 <= y && y < y1) {
				// Calculate x intersection
				x := x1 + (y-y1)*(x2-x1)/(y2-y1)
				intersections = append(intersections, x)
			}
		}

		// Sort intersections
		for i := 0; i < len(intersections)-1; i++ {
			for j := i + 1; j < len(intersections); j++ {
				if intersections[i] > intersections[j] {
					intersections[i], intersections[j] = intersections[j], intersections[i]
				}
			}
		}

		// Fill between pairs of intersections
		for i := 0; i < len(intersections)-1; i += 2 {
			x1 := intersections[i]
			x2 := intersections[i+1]
			for x := x1; x <= x2; x++ {
				if x >= 0 && x < img.Bounds().Max.X && y >= 0 && y < img.Bounds().Max.Y {
					img.Set(x, y, color.White)
				}
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
