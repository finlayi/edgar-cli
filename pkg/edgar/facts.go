package edgar

import (
	"context"
	"sort"
)

type FactPoint map[string]any

type ConceptData struct {
	Label       string                 `json:"label"`
	Description string                 `json:"description"`
	Units       map[string][]FactPoint `json:"units"`
}

type TaxonomyFacts map[string]ConceptData

type CompanyFactsPayload struct {
	CIK        int                      `json:"cik"`
	EntityName string                   `json:"entityName"`
	Facts      map[string]TaxonomyFacts `json:"facts"`
}

type ConceptSummary struct {
	Concept   string   `json:"concept"`
	Label     *string  `json:"label"`
	UnitCount int      `json:"unit_count"`
	Units     []string `json:"units"`
}

type FactsGetParams struct {
	ID       string
	Taxonomy string
	Concept  string
	Unit     string
	Latest   bool
}

func runFactsGet(ctx context.Context, params FactsGetParams, context CommandContext) (CommandResult, error) {
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return CommandResult{}, err
	}
	url, err := companyFactsURL(context.SecClient.Hosts(), entity.CIK)
	if err != nil {
		return CommandResult{}, err
	}
	var payload CompanyFactsPayload
	if err := context.SecClient.FetchSECJSON(ctx, url, &payload); err != nil {
		return CommandResult{}, err
	}
	allFacts := payload.Facts
	if allFacts == nil {
		allFacts = map[string]TaxonomyFacts{}
	}

	if params.Concept == "" {
		if params.Taxonomy != "" {
			taxonomyFacts, ok := allFacts[params.Taxonomy]
			if !ok {
				return CommandResult{}, NewCLIError(ErrorNotFound, "Taxonomy "+params.Taxonomy+" not found")
			}
			return CommandResult{Data: map[string]any{
				"cik":           entity.CIK,
				"entityName":    payload.EntityName,
				"taxonomy":      params.Taxonomy,
				"concept_count": len(taxonomyFacts),
				"concepts":      buildConceptSummary(taxonomyFacts),
			}}, nil
		}

		summary := map[string]any{}
		for taxonomy, taxonomyFacts := range allFacts {
			summary[taxonomy] = map[string]any{"concept_count": len(taxonomyFacts)}
		}
		return CommandResult{Data: map[string]any{
			"cik":        entity.CIK,
			"entityName": payload.EntityName,
			"taxonomies": summary,
		}}, nil
	}

	taxonomy, err := selectTaxonomy(allFacts, params.Concept, params.Taxonomy)
	if err != nil {
		return CommandResult{}, err
	}
	conceptData, ok := allFacts[taxonomy][params.Concept]
	if !ok {
		return CommandResult{}, NewCLIError(ErrorNotFound, "Concept "+params.Concept+" not found in taxonomy "+taxonomy)
	}

	rawUnits := conceptData.Units
	if rawUnits == nil {
		rawUnits = map[string][]FactPoint{}
	}
	selectedUnits := rawUnits
	if params.Unit != "" {
		points, ok := rawUnits[params.Unit]
		if !ok {
			return CommandResult{}, NewCLIError(ErrorNotFound, "Unit "+params.Unit+" not found for "+taxonomy+":"+params.Concept)
		}
		selectedUnits = map[string][]FactPoint{params.Unit: points}
	}

	label := stringPointerOrNil(conceptData.Label)
	description := stringPointerOrNil(conceptData.Description)
	if params.Latest {
		latestByUnit := map[string]any{}
		for unitName, points := range selectedUnits {
			latestByUnit[unitName] = pickLatest(points)
		}
		return CommandResult{Data: map[string]any{
			"cik":         entity.CIK,
			"entityName":  payload.EntityName,
			"taxonomy":    taxonomy,
			"concept":     params.Concept,
			"label":       label,
			"description": description,
			"latest":      latestByUnit,
		}}, nil
	}

	return CommandResult{Data: map[string]any{
		"cik":         entity.CIK,
		"entityName":  payload.EntityName,
		"taxonomy":    taxonomy,
		"concept":     params.Concept,
		"label":       label,
		"description": description,
		"units":       selectedUnits,
	}}, nil
}

func buildConceptSummary(taxonomyFacts TaxonomyFacts) []ConceptSummary {
	concepts := make([]ConceptSummary, 0, len(taxonomyFacts))
	for concept, payload := range taxonomyFacts {
		units := make([]string, 0, len(payload.Units))
		for unit := range payload.Units {
			units = append(units, unit)
		}
		sort.Strings(units)
		concepts = append(concepts, ConceptSummary{
			Concept:   concept,
			Label:     stringPointerOrNil(payload.Label),
			UnitCount: len(units),
			Units:     units,
		})
	}
	sort.Slice(concepts, func(i, j int) bool {
		return concepts[i].Concept < concepts[j].Concept
	})
	return concepts
}

func pickLatest(points []FactPoint) FactPoint {
	if len(points) == 0 {
		return nil
	}
	sorted := append([]FactPoint(nil), points...)
	sort.Slice(sorted, func(i, j int) bool {
		return factSortKey(sorted[i]) > factSortKey(sorted[j])
	})
	return sorted[0]
}

func factSortKey(point FactPoint) string {
	if value, ok := point["filed"].(string); ok {
		return value
	}
	if value, ok := point["end"].(string); ok {
		return value
	}
	return ""
}

func selectTaxonomy(allFacts map[string]TaxonomyFacts, concept string, taxonomy string) (string, error) {
	if taxonomy != "" {
		if _, ok := allFacts[taxonomy]; !ok {
			return "", NewCLIError(ErrorNotFound, "Taxonomy "+taxonomy+" not found")
		}
		return taxonomy, nil
	}

	for _, preferred := range []string{"us-gaap", "dei"} {
		if facts, ok := allFacts[preferred]; ok {
			if _, hasConcept := facts[concept]; hasConcept {
				return preferred, nil
			}
		}
	}
	for tax, facts := range allFacts {
		if _, hasConcept := facts[concept]; hasConcept {
			return tax, nil
		}
	}
	return "", NewCLIError(ErrorNotFound, "Concept "+concept+" not found in company facts")
}
