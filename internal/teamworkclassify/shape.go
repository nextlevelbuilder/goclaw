package teamworkclassify

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type WorkShape string

const (
	WorkShapeAtomic           WorkShape = "atomic"
	WorkShapeCrossCapability  WorkShape = "cross_capability"
	WorkShapeStaged           WorkShape = "staged"
	WorkShapeReviewedDecision WorkShape = "reviewed_decision"
)

type ShapeTraitType string

const (
	ShapeTraitSingleBoundedOutput     ShapeTraitType = "single_bounded_output"
	ShapeTraitMultipleCapabilities    ShapeTraitType = "multiple_capabilities"
	ShapeTraitSequentialDependency    ShapeTraitType = "sequential_dependency"
	ShapeTraitScoreOrRank             ShapeTraitType = "score_or_rank"
	ShapeTraitRecommendOrSelect       ShapeTraitType = "recommend_or_select"
	ShapeTraitIndependentVerification ShapeTraitType = "independent_verification"
	ShapeTraitExplicitCritique        ShapeTraitType = "explicit_critique"
)

type ShapeEvidenceSource string

const (
	ShapeEvidenceCurrentRequest ShapeEvidenceSource = "current_request"
	ShapeEvidencePinnedSkill    ShapeEvidenceSource = "pinned_skill"
)

type ShapeTrait struct {
	Type     ShapeTraitType      `json:"type"`
	Source   ShapeEvidenceSource `json:"source"`
	Evidence string              `json:"evidence"`
}

type ShapeAssessment struct {
	WorkShape                 WorkShape    `json:"work_shape"`
	ShapeTraits               []ShapeTrait `json:"shape_traits"`
	IndependentReviewRequired bool         `json:"independent_review_required"`
}

func ValidateShapeAssessment(input Input, assessment ShapeAssessment) (ShapeAssessment, error) {
	if len(assessment.ShapeTraits) == 0 {
		return assessment, fmt.Errorf("shape_traits is required")
	}
	seen := make(map[ShapeTraitType]struct{}, len(assessment.ShapeTraits))
	for i := range assessment.ShapeTraits {
		trait := &assessment.ShapeTraits[i]
		trait.Evidence = normalizeEvidenceText(trait.Evidence)
		if !validShapeTrait(trait.Type) {
			return assessment, fmt.Errorf("invalid shape trait %q", trait.Type)
		}
		if trait.Source != ShapeEvidenceCurrentRequest && trait.Source != ShapeEvidencePinnedSkill {
			return assessment, fmt.Errorf("invalid shape evidence source %q", trait.Source)
		}
		if trait.Evidence == "" {
			return assessment, fmt.Errorf("shape trait %q is missing evidence", trait.Type)
		}
		source := input.Message
		if trait.Source == ShapeEvidencePinnedSkill {
			source = input.PinnedSkillsContext
		}
		if !containsNormalizedEvidence(source, trait.Evidence) {
			return assessment, fmt.Errorf("shape trait %q evidence is not present in %s", trait.Type, trait.Source)
		}
		seen[trait.Type] = struct{}{}
	}
	derived := DeriveWorkShape(assessment.ShapeTraits)
	if assessment.WorkShape != derived {
		return assessment, fmt.Errorf("work_shape %q does not match validated traits (%s)", assessment.WorkShape, derived)
	}
	review := EffectiveReviewRequired(derived, assessment.ShapeTraits)
	if assessment.IndependentReviewRequired != review {
		return assessment, fmt.Errorf("independent_review_required does not match validated traits")
	}
	return assessment, nil
}

// DeriveWorkShape maps verified traits to a work shape. Narrowed policy: only
// independent_verification and explicit_critique escalate to reviewed_decision
// (they name a distinct reviewer of separately produced work). score_or_rank and
// recommend_or_select are DESCRIPTIVE traits — a single owner can score, rank,
// recommend, or select without a second reviewer — so they no longer force
// reviewed_decision. This is the core over-selection fix: broad
// research/comparison/recommendation must not by itself pull work into Team Work.
func DeriveWorkShape(traits []ShapeTrait) WorkShape {
	shape := WorkShapeAtomic
	for _, trait := range traits {
		switch trait.Type {
		case ShapeTraitIndependentVerification, ShapeTraitExplicitCritique:
			return WorkShapeReviewedDecision
		case ShapeTraitSequentialDependency:
			if shape != WorkShapeReviewedDecision {
				shape = WorkShapeStaged
			}
		case ShapeTraitMultipleCapabilities:
			if shape == WorkShapeAtomic {
				shape = WorkShapeCrossCapability
			}
		}
	}
	return shape
}

func EffectiveReviewRequired(shape WorkShape, traits []ShapeTrait) bool {
	if shape == WorkShapeReviewedDecision {
		return true
	}
	for _, trait := range traits {
		if trait.Type == ShapeTraitIndependentVerification || trait.Type == ShapeTraitExplicitCritique {
			return true
		}
	}
	return false
}

func ShapeTraitTypes(traits []ShapeTrait) []string {
	values := make([]string, 0, len(traits))
	for _, trait := range traits {
		values = append(values, string(trait.Type))
	}
	return values
}

func ShapeTraitSources(traits []ShapeTrait) []string {
	values := make([]string, 0, len(traits))
	for _, trait := range traits {
		values = append(values, string(trait.Source))
	}
	return values
}

func ConservativeWorkShape(a, b WorkShape) WorkShape {
	if shapeRank(b) > shapeRank(a) {
		return b
	}
	return a
}

func shapeRank(shape WorkShape) int {
	switch shape {
	case WorkShapeReviewedDecision:
		return 4
	case WorkShapeStaged:
		return 3
	case WorkShapeCrossCapability:
		return 2
	case WorkShapeAtomic:
		return 1
	default:
		return 0
	}
}

func validShapeTrait(value ShapeTraitType) bool {
	switch value {
	case ShapeTraitSingleBoundedOutput, ShapeTraitMultipleCapabilities, ShapeTraitSequentialDependency,
		ShapeTraitScoreOrRank, ShapeTraitRecommendOrSelect, ShapeTraitIndependentVerification, ShapeTraitExplicitCritique:
		return true
	default:
		return false
	}
}

func normalizeEvidenceText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func containsNormalizedEvidence(source, evidence string) bool {
	source = strings.ToLower(normalizeEvidenceText(source))
	evidence = strings.ToLower(normalizeEvidenceText(evidence))
	return evidence != "" && strings.Contains(source, evidence)
}

func BuildShapeVerifierMessages(input Input) []providers.Message {
	system := `You independently verify the semantic work shape of one current user request.
Return ONLY JSON. Do not choose an owner, inspect roster availability, or answer the user.
Use all applicable traits and quote exact evidence from the declared source. Tag a trait only when the current request or a pinned skill rule actually asks for it.
Schema:
{"work_shape":"atomic|staged|cross_capability|reviewed_decision","shape_traits":[{"type":"single_bounded_output|multiple_capabilities|sequential_dependency|score_or_rank|recommend_or_select|independent_verification|explicit_critique","source":"current_request|pinned_skill","evidence":"exact excerpt"}],"independent_review_required":true}
Rules:
- reviewed_decision applies ONLY when the request asks for independent verification or explicit critique of separately produced work by someone other than the producer.
- score_or_rank and recommend_or_select are DESCRIPTIVE traits: a single author can score, rank, compare, recommend, or select. Tag them when present, but they do NOT by themselves make the work reviewed_decision and do NOT by themselves require an independent reviewer.
- staged applies when a later output requires an earlier output.
- cross_capability applies when genuinely distinct capability outputs are required without a strict dependency.
- atomic applies to one bounded output with none of the multi/review traits.
- independent_review_required is true ONLY for independent_verification or explicit_critique. Broad research, comparison, ranking, or recommendation alone is not independent review.
- Pinned skill text is optional context; use only relevant rules and quote them exactly.`
	var user strings.Builder
	user.WriteString("Current request:\n")
	user.WriteString(input.Message)
	if strings.TrimSpace(input.PinnedSkillsContext) != "" {
		user.WriteString("\n\nPinned skills:\n")
		user.WriteString(input.PinnedSkillsContext)
	}
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user.String()}}
}

// BuildShapeRepairMessages asks the shape verifier to correct one rejected
// reply. It mirrors BuildPlannerRepairMessages: the previous attempt and the
// concrete rejection reason are appended so the model repairs THAT error rather
// than re-guessing from scratch. It exists because the classifier defaults to
// each agent's own runtime model, and a model that emits prose around its JSON
// (or quotes evidence loosely) would otherwise collapse the whole
// classification to a degraded self — see [[project-goclaw-teamwork-progress]].
// The schema contract is restated because the rejection is usually a contract
// violation, not a reasoning error.
func BuildShapeRepairMessages(input Input, previous string, rejection error) []providers.Message {
	reason := "unknown validation error"
	if rejection != nil {
		reason = rejection.Error()
	}
	messages := BuildShapeVerifierMessages(input)
	return append(messages,
		providers.Message{Role: "assistant", Content: previous},
		providers.Message{Role: "system", Content: `Your previous reply was rejected: ` + reason + `
Return ONE corrected JSON object only — no prose, no code fence, no commentary.
Every trait's "evidence" must be an EXACT substring copied from the source you tag ("current_request" = the current request text, "pinned_skill" = the pinned skill text). Do not paraphrase, translate, summarize, or add ellipses.
Keep work_shape consistent with the traits you report, and set independent_review_required to true ONLY for independent_verification or explicit_critique.`},
	)
}

func ParseShapeAssessment(content string) (ShapeAssessment, error) {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return ShapeAssessment{}, err
	}
	var assessment ShapeAssessment
	if err := json.Unmarshal([]byte(raw), &assessment); err != nil {
		return ShapeAssessment{}, err
	}
	return assessment, nil
}
