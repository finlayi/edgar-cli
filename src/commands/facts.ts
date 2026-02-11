import { CLIError, ErrorCode } from '../core/errors.js';
import { CommandContext, CommandResult } from '../core/runtime.js';
import { companyFactsUrl } from '../sec/endpoints.js';
import { resolveEntity } from '../sec/ticker-map.js';

interface FactPoint {
  end?: string;
  val?: number | string;
  accn?: string;
  fy?: number;
  fp?: string;
  form?: string;
  filed?: string;
  frame?: string;
}

interface ConceptData {
  label?: string;
  description?: string;
  units?: Record<string, FactPoint[]>;
}

type TaxonomyFacts = Record<string, ConceptData>;

interface CompanyFactsPayload {
  cik: number;
  entityName: string;
  facts: Record<string, TaxonomyFacts>;
}

interface ConceptSummary {
  concept: string;
  label: string | null;
  unit_count: number;
  units: string[];
}

function buildConceptSummary(taxonomyFacts: TaxonomyFacts): ConceptSummary[] {
  return Object.entries(taxonomyFacts)
    .map(([concept, payload]) => {
      const units = Object.keys(payload.units ?? {});
      return {
        concept,
        label: payload.label ?? null,
        unit_count: units.length,
        units
      };
    })
    .sort((a, b) => a.concept.localeCompare(b.concept));
}

function pickLatest(points: FactPoint[]): FactPoint | null {
  if (points.length === 0) {
    return null;
  }

  const sorted = [...points].sort((a, b) => {
    const aKey = a.filed ?? a.end ?? '';
    const bKey = b.filed ?? b.end ?? '';
    return bKey.localeCompare(aKey);
  });

  return sorted[0] ?? null;
}

function selectTaxonomy(
  allFacts: Record<string, TaxonomyFacts>,
  concept: string,
  taxonomy?: string
): string {
  if (taxonomy) {
    if (!allFacts[taxonomy]) {
      throw new CLIError(ErrorCode.NOT_FOUND, `Taxonomy ${taxonomy} not found`);
    }

    return taxonomy;
  }

  const preferred = ['us-gaap', 'dei'];
  for (const tax of preferred) {
    if (allFacts[tax]?.[concept]) {
      return tax;
    }
  }

  const anyTaxonomy = Object.keys(allFacts).find((tax) => Boolean(allFacts[tax]?.[concept]));
  if (anyTaxonomy) {
    return anyTaxonomy;
  }

  throw new CLIError(ErrorCode.NOT_FOUND, `Concept ${concept} not found in company facts`);
}

export async function runFactsGet(
  params: {
    id: string;
    taxonomy?: 'us-gaap' | 'dei';
    concept?: string;
    unit?: string;
    latest?: boolean;
  },
  context: CommandContext
): Promise<CommandResult> {
  const entity = await resolveEntity(params.id, context.secClient, { strictMapMatch: false });

  const payload = await context.secClient.fetchSecJson<CompanyFactsPayload>(companyFactsUrl(entity.cik));
  const allFacts = payload.facts ?? {};

  if (!params.concept) {
    if (params.taxonomy) {
      const taxonomyFacts = allFacts[params.taxonomy];
      if (!taxonomyFacts) {
        throw new CLIError(ErrorCode.NOT_FOUND, `Taxonomy ${params.taxonomy} not found`);
      }

      return {
        data: {
          cik: entity.cik,
          entityName: payload.entityName,
          taxonomy: params.taxonomy,
          concept_count: Object.keys(taxonomyFacts).length,
          concepts: buildConceptSummary(taxonomyFacts)
        }
      };
    }

    const taxonomySummary = Object.fromEntries(
      Object.entries(allFacts).map(([taxonomy, taxonomyFacts]) => [
        taxonomy,
        {
          concept_count: Object.keys(taxonomyFacts).length
        }
      ])
    );

    return {
      data: {
        cik: entity.cik,
        entityName: payload.entityName,
        taxonomies: taxonomySummary
      }
    };
  }

  const concept = params.concept;
  const taxonomy = selectTaxonomy(allFacts, concept, params.taxonomy);
  const conceptData = allFacts[taxonomy]?.[concept];

  if (!conceptData) {
    throw new CLIError(ErrorCode.NOT_FOUND, `Concept ${concept} not found in taxonomy ${taxonomy}`);
  }

  const rawUnits = conceptData.units ?? {};
  let selectedUnits: Record<string, FactPoint[]>;

  if (params.unit) {
    if (!rawUnits[params.unit]) {
      throw new CLIError(
        ErrorCode.NOT_FOUND,
        `Unit ${params.unit} not found for ${taxonomy}:${concept}`
      );
    }

    selectedUnits = {
      [params.unit]: rawUnits[params.unit]
    };
  } else {
    selectedUnits = rawUnits;
  }

  if (params.latest) {
    const latestByUnit: Record<string, FactPoint | null> = Object.fromEntries(
      Object.entries(selectedUnits).map(([unitName, points]) => [unitName, pickLatest(points)])
    );

    return {
      data: {
        cik: entity.cik,
        entityName: payload.entityName,
        taxonomy,
        concept,
        label: conceptData.label ?? null,
        description: conceptData.description ?? null,
        latest: latestByUnit
      }
    };
  }

  return {
    data: {
      cik: entity.cik,
      entityName: payload.entityName,
      taxonomy,
      concept,
      label: conceptData.label ?? null,
      description: conceptData.description ?? null,
      units: selectedUnits
    }
  };
}
