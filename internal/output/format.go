package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/driftctl/driftctl/internal/model"
)

// Format renders a drift report.
func Format(w io.Writer, report *model.DriftReport, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "table":
		return writeTable(w, report)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}

func writeTable(w io.Writer, report *model.DriftReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Scan ID:\t%s\n", report.ScanID)
	fmt.Fprintf(tw, "Workspace:\t%s\n", report.Workspace)
	fmt.Fprintf(tw, "Status:\t%s\n", report.Status)
	fmt.Fprintf(tw, "Started:\t%s\n", report.StartedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(tw, "Completed:\t%s\n", report.CompletedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(tw, "")
	fmt.Fprintln(tw, "SUMMARY")
	fmt.Fprintf(tw, "Total Resources:\t%d\n", report.Summary.TotalResources)
	fmt.Fprintf(tw, "Missing in Cloud:\t%d\n", report.Summary.MissingInCloud)
	fmt.Fprintf(tw, "Extra in Cloud:\t%d\n", report.Summary.ExtraInCloud)
	fmt.Fprintf(tw, "Attribute Changes:\t%d\n", report.Summary.AttributeChanges)
	fmt.Fprintf(tw, "Tag Changes:\t%d\n", report.Summary.TagChanges)
	fmt.Fprintf(tw, "Total Findings:\t%d\n", report.Summary.TotalFindings)
	fmt.Fprintln(tw, "")

	if len(report.Findings) == 0 {
		fmt.Fprintln(tw, "No drift detected.")
		return tw.Flush()
	}

	fmt.Fprintln(tw, "FINDINGS")
	fmt.Fprintln(tw, "KIND\tSEVERITY\tRESOURCE\tFIELD\tEXPECTED\tACTUAL")
	for _, f := range report.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s (%s)\t%s\t%v\t%v\n",
			f.Kind, f.Severity, f.ResourceName, f.ResourceType, f.Field, f.Expected, f.Actual)
	}
	return tw.Flush()
}

// HasDrift returns true if the report contains actionable drift.
func HasDrift(report *model.DriftReport) bool {
	return report.Summary.TotalFindings > 0
}
