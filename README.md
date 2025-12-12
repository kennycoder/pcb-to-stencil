# PCB to Stencil Converter

A Go tool to convert Gerber files (specifically solder paste layers) into 3D printable STL stencils.

## Features

- Parses standard RS-274X Gerber files.
- Supports standard apertures (Circle, Rectangle, Obround).
- Supports Aperture Macros (AM) with rotation (e.g., rounded rectangles).
- Automatically crops the output to the PCB bounds.
- Generates a 3D STL mesh optimized for 3D printing.

## Usage

Run the tool using `go run`:

```bash
go run main.go gerber.go [options] <path_to_gerber_file>
```

### Options

- `--height, -h`: Stencil height in mm (default: 0.2mm).
- `--keep-png, --kp`: Save the intermediate PNG image used for mesh generation (useful for debugging).

### Example

```bash
go run main.go gerber.go -height=0.25 -keep-png my_board_paste_top.gbr
```

This will generate `my_board_paste_top.stl` in the same directory.

## How it Works

1.  **Parsing**: The tool reads the Gerber file and interprets the drawing commands (flashes and draws).
2.  **Rendering**: It renders the PCB layer into a high-resolution internal image.
3.  **Meshing**: It converts the image into a 3D mesh using a run-length encoding approach to optimize the triangle count.
4.  **Export**: The mesh is saved as a binary STL file.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.