package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// CreateXlsxTool generates xlsx files from structured JSON data server-side.
// This eliminates the need for LLMs to generate Python scripts with hardcoded data,
// reducing output tokens by ~80% (JSON payload vs full Python script).
type CreateXlsxTool struct{}

func NewCreateXlsxTool() *CreateXlsxTool { return &CreateXlsxTool{} }

func (t *CreateXlsxTool) Name() string { return "create_xlsx" }

func (t *CreateXlsxTool) Description() string {
	return "Create an Excel (.xlsx) file from structured data. Pass headers, rows, and optional formatting as JSON — no Python script needed. " +
		"Returns a MEDIA: path to the generated file. Cell values starting with '=' are treated as formulas."
}

func (t *CreateXlsxTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filename_hint": map[string]any{
				"type":        "string",
				"description": "Short descriptive filename without extension. Example: 'sales-report-2026-q1'.",
			},
			"sheets": map[string]any{
				"type":        "array",
				"description": "Array of sheet definitions.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Sheet name.",
						},
						"headers": map[string]any{
							"type":        "array",
							"description": "Column header strings.",
							"items":       map[string]any{"type": "string"},
						},
						"rows": map[string]any{
							"type":        "array",
							"description": "2D array of cell values. Strings starting with '=' are formulas.",
							"items": map[string]any{
								"type":  "array",
								"items": map[string]any{},
							},
						},
						"column_widths": map[string]any{
							"type":        "array",
							"description": "Optional column widths (numbers). Omit for auto.",
							"items":       map[string]any{"type": "number"},
						},
					},
					"required": []string{"name", "headers", "rows"},
				},
			},
			"formatting": map[string]any{
				"type":        "object",
				"description": "Optional formatting options.",
				"properties": map[string]any{
					"header_color": map[string]any{
						"type":        "string",
						"description": "Header background color hex (e.g. '1F4E79'). Default: '1F4E79'.",
					},
					"number_format": map[string]any{
						"type":        "string",
						"description": "Number format for numeric cells (e.g. '#,##0'). Default: '#,##0'.",
					},
					"freeze_header": map[string]any{
						"type":        "boolean",
						"description": "Freeze the header row. Default: true.",
					},
					"auto_filter": map[string]any{
						"type":        "boolean",
						"description": "Enable auto-filter on headers. Default: true.",
					},
					"row_colors": map[string]any{
						"type":        "object",
						"description": "Conditional row coloring. Keys are column indices (0-based), values are objects mapping cell value to hex color. Example: {\"1\": {\"Nhập kho\": \"E2EFDA\", \"Xuất kho\": \"FCE4EC\"}}.",
					},
				},
			},
		},
		"required": []string{"sheets"},
	}
}

func (t *CreateXlsxTool) Execute(ctx context.Context, args map[string]any) *Result {
	sheetsRaw, ok := args["sheets"]
	if !ok {
		return ErrorResult("sheets is required")
	}

	filenameHint, _ := args["filename_hint"].(string)

	// Build the full spec as JSON for the Python script
	spec := map[string]any{"sheets": sheetsRaw}
	if fmt, ok := args["formatting"]; ok {
		spec["formatting"] = fmt
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to marshal spec: %v", err))
	}

	// Write spec to temp file
	specFile, err := os.CreateTemp("", "xlsx-spec-*.json")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create temp file: %v", err))
	}
	specPath := specFile.Name()
	defer os.Remove(specPath)

	if _, err := specFile.Write(specJSON); err != nil {
		specFile.Close()
		return ErrorResult(fmt.Sprintf("failed to write spec: %v", err))
	}
	specFile.Close()

	// Determine output path
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = os.TempDir()
	}
	dateDir := filepath.Join(workspace, "generated", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	outPath := filepath.Join(dateDir, mediaFileName(ctx, "xlsx", filenameHint, "xlsx"))

	// Run embedded Python script
	cmd := exec.CommandContext(ctx, "python3", "-c", createXlsxPythonScript, specPath, outPath)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("xlsx generation failed: %v\n%s", err, string(output)))
	}

	// Verify file
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return ErrorResult(fmt.Sprintf("xlsx file not created or empty: %v", err))
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nExcel file created: %s (%d bytes)", outPath, filepath.Base(outPath), info.Size())}
	result.Media = []bus.MediaFile{{Path: outPath, MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}}
	result.Deliverable = fmt.Sprintf("[Generated Excel: %s]", filepath.Base(outPath))
	return result
}

// createXlsxPythonScript is the embedded Python script that reads a JSON spec and creates an xlsx.
const createXlsxPythonScript = `
import json, sys
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
from openpyxl.utils import get_column_letter

spec_path, out_path = sys.argv[1], sys.argv[2]
with open(spec_path) as f:
    spec = json.load(f)

fmt_opts = spec.get("formatting", {})
hdr_color = fmt_opts.get("header_color", "1F4E79")
num_fmt = fmt_opts.get("number_format", "#,##0")
freeze = fmt_opts.get("freeze_header", True)
auto_filt = fmt_opts.get("auto_filter", True)
row_colors = fmt_opts.get("row_colors", {})

hdr_font = Font(bold=True, color="FFFFFF")
hdr_fill = PatternFill("solid", start_color=hdr_color)
hdr_align = Alignment(horizontal="center", vertical="center", wrap_text=True)
thin = Side(style="thin", color="CCCCCC")
border = Border(left=thin, right=thin, top=thin, bottom=thin)

wb = Workbook()
first = True

for sheet_def in spec["sheets"]:
    if first:
        ws = wb.active
        ws.title = sheet_def["name"]
        first = False
    else:
        ws = wb.create_sheet(sheet_def["name"])

    headers = sheet_def["headers"]
    rows = sheet_def["rows"]
    col_widths = sheet_def.get("column_widths", [])

    # Headers
    ws.append(headers)
    for ci in range(1, len(headers) + 1):
        cell = ws.cell(row=1, column=ci)
        cell.font = hdr_font
        cell.fill = hdr_fill
        cell.alignment = hdr_align
        cell.border = border
    ws.row_dimensions[1].height = 28

    # Data rows
    for ri, row in enumerate(rows, 2):
        for ci, val in enumerate(row, 1):
            cell = ws.cell(row=ri, column=ci)
            if isinstance(val, str) and val.startswith("="):
                cell.value = val  # formula
            else:
                cell.value = val
            cell.border = border
            # Number formatting for numeric cells
            if isinstance(val, (int, float)):
                cell.number_format = num_fmt
                cell.alignment = Alignment(horizontal="right", vertical="center")
            else:
                cell.alignment = Alignment(vertical="center")

        # Conditional row coloring
        for col_idx_str, color_map in row_colors.items():
            col_idx = int(col_idx_str)
            if col_idx < len(row):
                cell_val = str(row[col_idx])
                if cell_val in color_map:
                    fill = PatternFill("solid", start_color=color_map[cell_val])
                    for ci in range(1, len(headers) + 1):
                        ws.cell(row=ri, column=ci).fill = fill

    # Column widths
    if col_widths:
        for i, w in enumerate(col_widths, 1):
            ws.column_dimensions[get_column_letter(i)].width = w
    else:
        # Auto-width based on header length
        for i, h in enumerate(headers, 1):
            ws.column_dimensions[get_column_letter(i)].width = max(len(str(h)) + 4, 12)

    if freeze:
        ws.freeze_panes = "A2"
    if auto_filt and rows:
        ws.auto_filter.ref = f"A1:{get_column_letter(len(headers))}{len(rows) + 1}"

wb.save(out_path)
print(f"OK: {len(spec['sheets'])} sheets, {sum(len(s['rows']) for s in spec['sheets'])} total rows")
`
