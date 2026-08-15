package skills

import (
	"fmt"
	"testing"
)

// BenchmarkSearch_10Skills benchmarks BM25 search over 10 skills.
func BenchmarkSearch_10Skills(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(10)
	idx.Build(skills)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("database query tool", 5)
	}
}

// BenchmarkSearch_100Skills benchmarks BM25 search over 100 skills.
func BenchmarkSearch_100Skills(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(100)
	idx.Build(skills)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("file system operations", 5)
	}
}

// BenchmarkSearch_500Skills benchmarks BM25 search over 500 skills.
func BenchmarkSearch_500Skills(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(500)
	idx.Build(skills)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("api http request handling", 5)
	}
}

// BenchmarkSearch_MultiTerm benchmarks search with multi-term query.
func BenchmarkSearch_MultiTerm(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(100)
	idx.Build(skills)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("advanced machine learning training model", 5)
	}
}

// BenchmarkSearch_SingleTerm benchmarks search with single-term query.
func BenchmarkSearch_SingleTerm(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(100)
	idx.Build(skills)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Search("database", 5)
	}
}

// BenchmarkBuild_100Skills benchmarks building the index for 100 skills.
func BenchmarkBuild_100Skills(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(100)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Build(skills)
	}
}

// BenchmarkBuild_500Skills benchmarks building the index for 500 skills.
func BenchmarkBuild_500Skills(b *testing.B) {
	idx := NewIndex()
	skills := makeSkillCorpus(500)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.Build(skills)
	}
}

// makeSkillCorpus creates a mock corpus of skills with realistic descriptions.
func makeSkillCorpus(count int) []Info {
	skillTemplates := []struct {
		name, desc string
	}{
		{"database_query", "Execute SQL queries and retrieve data from relational databases"},
		{"file_operations", "Read, write, and manage files on the filesystem"},
		{"http_requests", "Make HTTP requests to APIs and handle responses"},
		{"text_processing", "Parse and manipulate text documents"},
		{"json_parsing", "Parse and generate JSON structures"},
		{"csv_export", "Export data to CSV format"},
		{"authentication", "Manage user authentication and authorization"},
		{"caching", "Cache frequently accessed data"},
		{"logging", "Log events and debug information"},
		{"error_handling", "Handle and recover from errors"},
		{"machine_learning", "Train and execute machine learning models"},
		{"data_validation", "Validate input data against schemas"},
		{"scheduling", "Schedule tasks and manage timers"},
		{"networking", "Handle network communication and protocols"},
		{"encryption", "Encrypt and decrypt sensitive data"},
	}

	skills := make([]Info, 0, count)
	for i := 0; i < count; i++ {
		tmpl := skillTemplates[i%len(skillTemplates)]
		slug := fmt.Sprintf("%s_%d", tmpl.name, i)

		desc := tmpl.desc
		if i%3 == 0 {
			desc += " with advanced options for tuning and optimization"
		}
		if i%5 == 0 {
			desc += " and integrates with external services"
		}

		skills = append(skills, Info{
			Name:        fmt.Sprintf("%s_%d", tmpl.name, i),
			Slug:        slug,
			Path:        fmt.Sprintf("/tmp/skills/%s/SKILL.md", slug),
			BaseDir:     fmt.Sprintf("/tmp/skills/%s", slug),
			Source:      "builtin",
			Description: desc,
		})
	}

	return skills
}
