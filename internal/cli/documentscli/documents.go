// Package documentscli is `monoagentcli documents`. What the commands need from the CLI around
// them comes in through Deps, so they can be tested in isolation. (The package is not named
// documents: it is built on internal/documents, which renders and stores them.)
package documentscli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/documents"
	"github.com/monoes/mono-agent/internal/storage"

	"github.com/spf13/cobra"
)

// Deps is what the commands take from the CLI around them.
type Deps struct {
	// JSONOutput reports the global --json flag; it is read when a command runs.
	JSONOutput func() bool
	// ProfileID is the profile in effect; read after OpenDB, which resolves --profile.
	ProfileID func() string
	// OpenDB opens (and migrates) the database and resolves the profile (the CLI's initDB).
	OpenDB func() (*storage.Database, error)
}

// NewCommand returns the `documents` command group: renders
// structured CV/cover-letter/tender-proposal data into HTML+PDF and saves
// both into the vault. See
// docs/mastermind/specs/2026-09-05-document-generation-design.md.
func NewCommand(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "documents",
		Short: "Generate CV, cover letter, and tender proposal documents",
	}
	cmd.AddCommand(newRenderCmd(d))
	return cmd
}

func newRenderCmd(d Deps) *cobra.Command {
	var docType, dataFile, applicationID string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render a document from structured JSON data",
		Example: `  monoagentcli documents render --type cv --data-file cv.json
  monoagentcli documents render --type cover_letter --data-file letter.json --application-id abc123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(dataFile)
			if err != nil {
				return fmt.Errorf("reading data file: %w", err)
			}

			var htmlDocID, pdfDocID string
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()

			switch documents.DocType(docType) {
			case documents.DocTypeCV:
				var data documents.CVData
				if err := json.Unmarshal(raw, &data); err != nil {
					return clicore.InvalidInput("parsing --data-file: %v", err)
				}
				htmlDocID, pdfDocID, err = documents.GenerateDocument(cmd.Context(), db.DB, d.ProfileID(), applicationID, documents.DocTypeCV, data)
			case documents.DocTypeCoverLetter:
				var data documents.CoverLetterData
				if err := json.Unmarshal(raw, &data); err != nil {
					return clicore.InvalidInput("parsing --data-file: %v", err)
				}
				htmlDocID, pdfDocID, err = documents.GenerateDocument(cmd.Context(), db.DB, d.ProfileID(), applicationID, documents.DocTypeCoverLetter, data)
			case documents.DocTypeTenderProposal:
				var data documents.TenderProposalData
				if err := json.Unmarshal(raw, &data); err != nil {
					return clicore.InvalidInput("parsing --data-file: %v", err)
				}
				htmlDocID, pdfDocID, err = documents.GenerateDocument(cmd.Context(), db.DB, d.ProfileID(), applicationID, documents.DocTypeTenderProposal, data)
			default:
				return clicore.InvalidInput("--type must be one of cv, cover_letter, tender_proposal, got %q", docType)
			}
			if err != nil {
				return fmt.Errorf("generating document: %w", err)
			}

			if d.JSONOutput() {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]string{"html_document_id": htmlDocID, "pdf_document_id": pdfDocID})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated %s: html=%s pdf=%s\n", docType, htmlDocID, pdfDocID)
			return nil
		},
	}
	cmd.Flags().StringVar(&docType, "type", "", "Document type: cv, cover_letter, or tender_proposal (required)")
	cmd.Flags().StringVar(&dataFile, "data-file", "", "Path to a JSON file matching the chosen type's fields (required)")
	cmd.Flags().StringVar(&applicationID, "application-id", "", "Optional job/tender application to link this document to")
	cmd.MarkFlagRequired("type")
	cmd.MarkFlagRequired("data-file")
	return cmd
}
