package teamworkclassify

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
