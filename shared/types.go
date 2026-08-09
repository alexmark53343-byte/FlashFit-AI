package shared

type Filament struct {
	Brand              string   `json:"brand"`
	Product            string   `json:"product"`
	Material           string   `json:"material"`
	Variant            string   `json:"variant"`
	NozzleMin          float64  `json:"nozzle_min"`
	NozzleMax          float64  `json:"nozzle_max"`
	NozzleDefault      float64  `json:"nozzle_default"`
	BedMin             float64  `json:"bed_min"`
	BedMax             float64  `json:"bed_max"`
	BedDefault         float64  `json:"bed_default"`
	FanMin             float64  `json:"fan_min"`
	FanMax             float64  `json:"fan_max"`
	MaxVolumetricSpeed float64  `json:"max_volumetric_speed"`
	Density            float64  `json:"density"`
	FlowRatio          float64  `json:"flow_ratio"`
	PressureAdvance    *float64 `json:"pressure_advance"`
	Confidence         string   `json:"confidence"`
	Source             string   `json:"source"`
	Notes              string   `json:"notes"`
	SourcePath         string   `json:"source_path,omitempty"`
	OfficialProfile    bool     `json:"official_profile,omitempty"`
}

type ModelAnalysis struct {
	InputFormat      string     `json:"input_format"`
	SourcePath       string     `json:"-"`
	SourceSHA256     string     `json:"source_sha256"`
	Sanitized        bool       `json:"sanitized"`
	ObjectCount      int        `json:"object_count"`
	Filename         string     `json:"filename"`
	SHA256           string     `json:"sha256"`
	SizeBytes        int64      `json:"size_bytes"`
	TriangleCount    int        `json:"triangle_count"`
	BoundsMin        [3]float64 `json:"bounds_min"`
	BoundsMax        [3]float64 `json:"bounds_max"`
	Extents          [3]float64 `json:"extents"`
	SurfaceArea      float64    `json:"surface_area"`
	Volume           float64    `json:"volume"`
	Watertight       bool       `json:"watertight"`
	DegenerateFaces  int        `json:"degenerate_faces"`
	OverhangRatio    float64    `json:"overhang_ratio"`
	BedContactRatio  float64    `json:"bed_contact_ratio"`
	ThinOrTall       bool       `json:"thin_or_tall"`
	SupportSuggested bool       `json:"support_suggested"`
	BrimSuggested    bool       `json:"brim_suggested"`
	Category         string     `json:"category"`
	Warnings         []string   `json:"warnings"`
	StoredModelPath  string     `json:"-"`
}

type Recommendation struct {
	Quality                  string             `json:"quality"`
	QualityLabel             string             `json:"quality_label"`
	Texture                  string             `json:"texture"`
	TextureLabel             string             `json:"texture_label"`
	Process                  map[string]any     `json:"process"`
	Filament                 map[string]any     `json:"filament"`
	Reasons                  []string           `json:"reasons"`
	Warnings                 []string           `json:"warnings"`
	EstimatedRelativeTime    float64            `json:"estimated_relative_time"`
	EstimatedBalancedMinutes float64            `json:"estimated_balanced_minutes"`
	EstimatedModeMinutes     float64            `json:"estimated_mode_minutes"`
	DurationClass            string             `json:"duration_class"`
	CriticalValues           map[string]float64 `json:"critical_values"`
	CriticalSettings         map[string]string  `json:"critical_settings"`
}
