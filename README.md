# Gerber Solder Paste Layer to Solder Stencil Converter

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
go run main.go gerber.go [options] <path_to_gerber_file> [optional_board_outline_file]
```

### Options

- `--height`: Stencil height in mm (default: 0.16mm).
- `--wall-height`: Wall height mm (default: 2.0mm).
- `--wall-thickness`: Wall thickness in mm (default: 1mm).
- `--keep-png`: Save the intermediate PNG image used for mesh generation (useful for debugging).
- `-server`: Start the web interface server.
- `-port`: Port to run the server on (default: 8080).

### Example

```bash
go run main.go gerber.go -height=0.16 -keep-png my_board_paste_top.gbr my_board_outline.gbr
```

This will generate `my_board_paste_top.stl` in the same directory.

### Web Interface

To start the web interface:

```bash
go run main.go gerber.go -server
```

Then open `http://localhost:8080` in your browser. You can upload files and configure settings via the UI.

## 3D Printing Recommendations

For optimal results with small SMD packages (like TSSOP, 0402, etc.), use the following 3D print settings:

-   **Nozzle Size**: 0.2mm (Highly recommended for sharp corners and fine apertures).
-   **Layer Height**: 0.16mm total height.
    -   **First Layer**: 0.10mm
    -   **Second Layer**: 0.06mm
-   **Build Surface**: Smooth PEI sheet (Ensures the bottom of the stencil is perfectly flat for good PCB adhesion).

These settings assume you run the tool with `-height=0.16` (the default).

## How it Works

1.  **Parsing**: The tool reads the Gerber file and interprets the drawing commands (flashes and draws).
2.  **Rendering**: It renders the PCB layer into a high-resolution internal image.
3.  **Meshing**: It converts the image into a 3D mesh using a run-length encoding approach to optimize the triangle count.
4.  **Export**: The mesh is saved as a binary STL file.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.