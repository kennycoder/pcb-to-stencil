package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// --- Configuration ---
const (
	DPI       = 1000.0 // Higher DPI = smoother curves
	PixelToMM = 25.4 / DPI
)

var StencilHeight float64 = 0.2 // mm, default
var KeepPNG bool

// --- STL Helpers ---

type Point struct {
	X, Y, Z float64
}

func WriteSTL(filename string, triangles [][3]Point) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Writing Binary STL is harder, ASCII is fine for this size
	f.WriteString("solid stencil\n")
	for _, t := range triangles {
		f.WriteString("facet normal 0 0 0\n")
		f.WriteString("  outer loop\n")
		for _, p := range t {
			f.WriteString(fmt.Sprintf("    vertex %f %f %f\n", p.X, p.Y, p.Z))
		}
		f.WriteString("  endloop\n")
		f.WriteString("endfacet\n")
	}
	f.WriteString("endsolid stencil\n")
	return nil
}

func AddBox(triangles *[][3]Point, x, y, w, h, zHeight float64) {
	x0, y0 := x, y
	x1, y1 := x+w, y+h
	z0, z1 := 0.0, zHeight

	p000 := Point{x0, y0, z0}
	p100 := Point{x1, y0, z0}
	p110 := Point{x1, y1, z0}
	p010 := Point{x0, y1, z0}
	p001 := Point{x0, y0, z1}
	p101 := Point{x1, y0, z1}
	p111 := Point{x1, y1, z1}
	p011 := Point{x0, y1, z1}

	addQuad := func(a, b, c, d Point) {
		*triangles = append(*triangles, [3]Point{a, b, c})
		*triangles = append(*triangles, [3]Point{c, d, a})
	}

	addQuad(p000, p010, p110, p100) // Bottom
	addQuad(p101, p111, p011, p001) // Top
	addQuad(p000, p100, p101, p001) // Front
	addQuad(p100, p110, p111, p101) // Right
	addQuad(p110, p010, p011, p111) // Back
	addQuad(p010, p000, p001, p011) // Left
}

// --- Meshing Logic (Optimized) ---

func GenerateMeshFromImage(img image.Image) [][3]Point {
	bounds := img.Bounds()
	width := bounds.Max.X
	height := bounds.Max.Y
	var triangles [][3]Point

	// Optimization: Run-Length Encoding
	for y := 0; y < height; y++ {
		var startX = -1

		for x := 0; x < width; x++ {
			c := img.At(x, y)
			r, g, b, _ := c.RGBA()

			// Check for BLACK pixels (The Plastic Stencil Body)
			// Adjust threshold if gerbv produces slightly gray blacks
			isSolid := r < 10000 && g < 10000 && b < 10000

			if isSolid {
				if startX == -1 {
					startX = x
				}
			} else {
				if startX != -1 {
					// End of strip, generate box
					stripLen := x - startX
					AddBox(
						&triangles,
						float64(startX)*PixelToMM,
						float64(y)*PixelToMM,
						float64(stripLen)*PixelToMM,
						PixelToMM,
						StencilHeight,
					)
					startX = -1
				}
			}
		}
		if startX != -1 {
			stripLen := width - startX
			AddBox(
				&triangles,
				float64(startX)*PixelToMM,
				float64(y)*PixelToMM,
				float64(stripLen)*PixelToMM,
				PixelToMM,
				StencilHeight,
			)
		}
	}
	return triangles
}

// --- Main ---

func main() {
	flag.Float64Var(&StencilHeight, "height", 0.2, "Stencil height in mm")
	flag.Float64Var(&StencilHeight, "h", 0.2, "Stencil height in mm (short)")
	flag.BoolVar(&KeepPNG, "keep-png", false, "Save intermediate PNG file")
	flag.BoolVar(&KeepPNG, "kp", false, "Save intermediate PNG file (short)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [options] <path_to_gerber_file>")
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println("Example: go run main.go -height=0.3 MyPCB.GTP")
		os.Exit(1)
	}

	gerberPath := args[0]
	outputPath := strings.TrimSuffix(gerberPath, filepath.Ext(gerberPath)) + ".stl"

	// 1. Parse Gerber
	fmt.Printf("Parsing %s...\n", gerberPath)
	gf, err := ParseGerber(gerberPath)
	if err != nil {
		log.Fatalf("Error parsing gerber: %v", err)
	}

	// 2. Render to Image
	fmt.Println("Rendering to internal image...")
	img := gf.Render(DPI)

	if KeepPNG {
		pngPath := strings.TrimSuffix(gerberPath, filepath.Ext(gerberPath)) + ".png"
		fmt.Printf("Saving intermediate PNG to %s...\n", pngPath)
		f, err := os.Create(pngPath)
		if err != nil {
			log.Printf("Warning: Could not create PNG file: %v", err)
		} else {
			if err := png.Encode(f, img); err != nil {
				log.Printf("Warning: Could not encode PNG: %v", err)
			}
			f.Close()
		}
	}

	// 3. Generate Mesh
	fmt.Println("Generating mesh (this may take 10-20 seconds for large boards)...")
	triangles := GenerateMeshFromImage(img)

	// 4. Save STL
	fmt.Printf("Saving to %s (%d triangles)...\n", outputPath, len(triangles))
	err = WriteSTL(outputPath, triangles)
	if err != nil {
		log.Fatalf("Error writing STL: %v", err)
	}

	fmt.Println("Success! Happy printing.")
}
