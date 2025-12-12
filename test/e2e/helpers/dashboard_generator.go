//go:build e2e
// +build e2e

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helpers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"
)

// DashboardGenerator creates HTML dashboards for test results
type DashboardGenerator struct {
	outputDir string
}

// DashboardData represents data for the dashboard template
type DashboardData struct {
	Title       string        `json:"title"`
	GeneratedAt time.Time     `json:"generatedAt"`
	TestSuites  []*TestReport `json:"testSuites"`
	Summary     TestSummary   `json:"summary"`
	Charts      ChartData     `json:"charts"`
}

// TestSummary provides overall test statistics
type TestSummary struct {
	TotalSuites     int           `json:"totalSuites"`
	TotalTests      int           `json:"totalTests"`
	PassedTests     int           `json:"passedTests"`
	FailedTests     int           `json:"failedTests"`
	SkippedTests    int           `json:"skippedTests"`
	SuccessRate     float64       `json:"successRate"`
	TotalDuration   time.Duration `json:"totalDuration"`
	AverageDuration time.Duration `json:"averageDuration"`
}

// ChartData provides data for charts and visualizations
type ChartData struct {
	SuiteResults  []SuiteResult   `json:"suiteResults"`
	DurationChart []DurationPoint `json:"durationChart"`
	TrendData     []TrendPoint    `json:"trendData"`
}

// SuiteResult represents results for a single test suite
type SuiteResult struct {
	Name     string  `json:"name"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Skipped  int     `json:"skipped"`
	Duration float64 `json:"duration"`
}

// DurationPoint represents a point in the duration chart
type DurationPoint struct {
	Suite    string  `json:"suite"`
	Duration float64 `json:"duration"`
}

// TrendPoint represents a point in the trend chart
type TrendPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	SuccessRate float64   `json:"successRate"`
	Duration    float64   `json:"duration"`
}

// NewDashboardGenerator creates a new dashboard generator
func NewDashboardGenerator(outputDir string) *DashboardGenerator {
	return &DashboardGenerator{
		outputDir: outputDir,
	}
}

// GenerateDashboard creates an HTML dashboard from test reports
func (d *DashboardGenerator) GenerateDashboard(reports []*TestReport) error {
	// Ensure output directory exists
	if err := os.MkdirAll(d.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Prepare dashboard data
	dashboardData := d.prepareDashboardData(reports)

	// Generate HTML dashboard
	if err := d.generateHTMLDashboard(dashboardData); err != nil {
		return fmt.Errorf("failed to generate HTML dashboard: %w", err)
	}

	// Generate JSON data file
	if err := d.generateJSONData(dashboardData); err != nil {
		return fmt.Errorf("failed to generate JSON data: %w", err)
	}

	// Copy static assets
	if err := d.copyStaticAssets(); err != nil {
		return fmt.Errorf("failed to copy static assets: %w", err)
	}

	return nil
}

// prepareDashboardData prepares data for the dashboard
func (d *DashboardGenerator) prepareDashboardData(reports []*TestReport) *DashboardData {
	summary := d.calculateSummary(reports)
	charts := d.prepareChartData(reports)

	return &DashboardData{
		Title:       "OptipPod E2E Test Results",
		GeneratedAt: time.Now(),
		TestSuites:  reports,
		Summary:     summary,
		Charts:      charts,
	}
}

// calculateSummary calculates overall test statistics
func (d *DashboardGenerator) calculateSummary(reports []*TestReport) TestSummary {
	summary := TestSummary{
		TotalSuites: len(reports),
	}

	var totalDuration time.Duration

	for _, report := range reports {
		summary.TotalTests += len(report.TestCases)
		totalDuration += report.Duration

		for _, testCase := range report.TestCases {
			switch testCase.Status {
			case "passed":
				summary.PassedTests++
			case "failed":
				summary.FailedTests++
			case "skipped":
				summary.SkippedTests++
			}
		}
	}

	summary.TotalDuration = totalDuration
	if summary.TotalSuites > 0 {
		summary.AverageDuration = totalDuration / time.Duration(summary.TotalSuites)
	}

	if summary.TotalTests > 0 {
		summary.SuccessRate = float64(summary.PassedTests) / float64(summary.TotalTests) * 100
	}

	return summary
}

// prepareChartData prepares data for charts and visualizations
func (d *DashboardGenerator) prepareChartData(reports []*TestReport) ChartData {
	charts := ChartData{
		SuiteResults:  make([]SuiteResult, 0, len(reports)),
		DurationChart: make([]DurationPoint, 0, len(reports)),
		TrendData:     make([]TrendPoint, 0, len(reports)),
	}

	for _, report := range reports {
		// Suite results
		suiteResult := SuiteResult{
			Name:     report.TestSuite,
			Duration: report.Duration.Seconds(),
		}

		for _, testCase := range report.TestCases {
			switch testCase.Status {
			case "passed":
				suiteResult.Passed++
			case "failed":
				suiteResult.Failed++
			case "skipped":
				suiteResult.Skipped++
			}
		}

		charts.SuiteResults = append(charts.SuiteResults, suiteResult)

		// Duration chart
		charts.DurationChart = append(charts.DurationChart, DurationPoint{
			Suite:    report.TestSuite,
			Duration: report.Duration.Seconds(),
		})

		// Trend data
		successRate := float64(0)
		if len(report.TestCases) > 0 {
			successRate = float64(suiteResult.Passed) / float64(len(report.TestCases)) * 100
		}

		charts.TrendData = append(charts.TrendData, TrendPoint{
			Timestamp:   report.StartTime,
			SuccessRate: successRate,
			Duration:    report.Duration.Seconds(),
		})
	}

	return charts
}

// generateHTMLDashboard generates the HTML dashboard
func (d *DashboardGenerator) generateHTMLDashboard(data *DashboardData) error {
	tmpl := template.Must(template.New("dashboard").Parse(dashboardTemplate))

	dashboardPath := filepath.Join(d.outputDir, "index.html")
	file, err := os.Create(dashboardPath)
	if err != nil {
		return fmt.Errorf("failed to create dashboard file: %w", err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute dashboard template: %w", err)
	}

	return nil
}

// generateJSONData generates JSON data file for external consumption
func (d *DashboardGenerator) generateJSONData(data *DashboardData) error {
	jsonPath := filepath.Join(d.outputDir, "test-data.json")

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal dashboard data: %w", err)
	}

	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON data: %w", err)
	}

	return nil
}

// copyStaticAssets copies static CSS and JS files
func (d *DashboardGenerator) copyStaticAssets() error {
	// Create CSS file
	cssPath := filepath.Join(d.outputDir, "dashboard.css")
	if err := os.WriteFile(cssPath, []byte(dashboardCSS), 0644); err != nil {
		return fmt.Errorf("failed to write CSS file: %w", err)
	}

	// Create JS file
	jsPath := filepath.Join(d.outputDir, "dashboard.js")
	if err := os.WriteFile(jsPath, []byte(dashboardJS), 0644); err != nil {
		return fmt.Errorf("failed to write JS file: %w", err)
	}

	return nil
}

// HTML template for the dashboard
const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="dashboard.css">
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head>
<body>
    <div class="container">
        <header>
            <h1>{{.Title}}</h1>
            <p class="generated-at">Generated at: {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</p>
        </header>

        <div class="summary-cards">
            <div class="card">
                <h3>Total Test Suites</h3>
                <div class="metric">{{.Summary.TotalSuites}}</div>
            </div>
            <div class="card">
                <h3>Total Tests</h3>
                <div class="metric">{{.Summary.TotalTests}}</div>
            </div>
            <div class="card success">
                <h3>Passed</h3>
                <div class="metric">{{.Summary.PassedTests}}</div>
            </div>
            <div class="card failure">
                <h3>Failed</h3>
                <div class="metric">{{.Summary.FailedTests}}</div>
            </div>
            <div class="card">
                <h3>Success Rate</h3>
                <div class="metric">{{printf "%.1f%%" .Summary.SuccessRate}}</div>
            </div>
            <div class="card">
                <h3>Total Duration</h3>
                <div class="metric">{{.Summary.TotalDuration}}</div>
            </div>
        </div>

        <div class="charts-section">
            <div class="chart-container">
                <h3>Test Results by Suite</h3>
                <canvas id="suiteResultsChart"></canvas>
            </div>
            <div class="chart-container">
                <h3>Duration by Suite</h3>
                <canvas id="durationChart"></canvas>
            </div>
        </div>

        <div class="test-suites">
            <h2>Test Suite Details</h2>
            {{range .TestSuites}}
            <div class="test-suite">
                <h3>{{.TestSuite}}</h3>
                <div class="suite-info">
                    <span class="status {{.Status}}">{{.Status}}</span>
                    <span class="duration">Duration: {{.Duration}}</span>
                    <span class="test-count">Tests: {{len .TestCases}}</span>
                </div>
                
                {{if .TestCases}}
                <div class="test-cases">
                    {{range .TestCases}}
                    <div class="test-case {{.Status}}">
                        <span class="test-name">{{.Name}}</span>
                        <span class="test-duration">{{.Duration}}</span>
                        {{if .ErrorMsg}}
                        <div class="error-message">{{.ErrorMsg}}</div>
                        {{end}}
                    </div>
                    {{end}}
                </div>
                {{end}}
            </div>
            {{end}}
        </div>
    </div>

    <script src="dashboard.js"></script>
    <script>
        // Initialize charts with data
        const chartData = {{.Charts | printf "%+v"}};
        initializeCharts(chartData);
    </script>
</body>
</html>`

// CSS styles for the dashboard
const dashboardCSS = `
body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    margin: 0;
    padding: 0;
    background-color: #f5f5f5;
    color: #333;
}

.container {
    max-width: 1200px;
    margin: 0 auto;
    padding: 20px;
}

header {
    text-align: center;
    margin-bottom: 30px;
}

header h1 {
    color: #2c3e50;
    margin-bottom: 10px;
}

.generated-at {
    color: #7f8c8d;
    font-size: 14px;
}

.summary-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 20px;
    margin-bottom: 40px;
}

.card {
    background: white;
    padding: 20px;
    border-radius: 8px;
    box-shadow: 0 2px 4px rgba(0,0,0,0.1);
    text-align: center;
}

.card h3 {
    margin: 0 0 10px 0;
    font-size: 14px;
    color: #7f8c8d;
    text-transform: uppercase;
}

.card .metric {
    font-size: 32px;
    font-weight: bold;
    color: #2c3e50;
}

.card.success .metric {
    color: #27ae60;
}

.card.failure .metric {
    color: #e74c3c;
}

.charts-section {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 30px;
    margin-bottom: 40px;
}

.chart-container {
    background: white;
    padding: 20px;
    border-radius: 8px;
    box-shadow: 0 2px 4px rgba(0,0,0,0.1);
}

.chart-container h3 {
    margin-top: 0;
    color: #2c3e50;
}

.test-suites {
    background: white;
    padding: 20px;
    border-radius: 8px;
    box-shadow: 0 2px 4px rgba(0,0,0,0.1);
}

.test-suite {
    margin-bottom: 30px;
    padding-bottom: 20px;
    border-bottom: 1px solid #ecf0f1;
}

.test-suite:last-child {
    border-bottom: none;
}

.test-suite h3 {
    margin: 0 0 10px 0;
    color: #2c3e50;
}

.suite-info {
    display: flex;
    gap: 20px;
    margin-bottom: 15px;
    font-size: 14px;
}

.status {
    padding: 4px 8px;
    border-radius: 4px;
    font-weight: bold;
    text-transform: uppercase;
}

.status.passed {
    background-color: #d5f4e6;
    color: #27ae60;
}

.status.failed {
    background-color: #fdf2f2;
    color: #e74c3c;
}

.status.running {
    background-color: #fff3cd;
    color: #856404;
}

.test-cases {
    margin-top: 15px;
}

.test-case {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
    border-bottom: 1px solid #f8f9fa;
}

.test-case:last-child {
    border-bottom: none;
}

.test-case.passed {
    color: #27ae60;
}

.test-case.failed {
    color: #e74c3c;
}

.test-name {
    flex: 1;
}

.test-duration {
    font-size: 12px;
    color: #7f8c8d;
}

.error-message {
    margin-top: 5px;
    padding: 10px;
    background-color: #fdf2f2;
    border-left: 4px solid #e74c3c;
    font-size: 12px;
    color: #721c24;
}

@media (max-width: 768px) {
    .charts-section {
        grid-template-columns: 1fr;
    }
    
    .suite-info {
        flex-direction: column;
        gap: 5px;
    }
}
`

// JavaScript for the dashboard
const dashboardJS = `
function initializeCharts(data) {
    // Suite Results Chart
    const suiteCtx = document.getElementById('suiteResultsChart').getContext('2d');
    new Chart(suiteCtx, {
        type: 'bar',
        data: {
            labels: data.suiteResults.map(s => s.name),
            datasets: [
                {
                    label: 'Passed',
                    data: data.suiteResults.map(s => s.passed),
                    backgroundColor: '#27ae60',
                },
                {
                    label: 'Failed',
                    data: data.suiteResults.map(s => s.failed),
                    backgroundColor: '#e74c3c',
                },
                {
                    label: 'Skipped',
                    data: data.suiteResults.map(s => s.skipped),
                    backgroundColor: '#f39c12',
                }
            ]
        },
        options: {
            responsive: true,
            scales: {
                x: {
                    stacked: true,
                },
                y: {
                    stacked: true,
                    beginAtZero: true
                }
            }
        }
    });

    // Duration Chart
    const durationCtx = document.getElementById('durationChart').getContext('2d');
    new Chart(durationCtx, {
        type: 'line',
        data: {
            labels: data.durationChart.map(d => d.suite),
            datasets: [{
                label: 'Duration (seconds)',
                data: data.durationChart.map(d => d.duration),
                borderColor: '#3498db',
                backgroundColor: 'rgba(52, 152, 219, 0.1)',
                tension: 0.1
            }]
        },
        options: {
            responsive: true,
            scales: {
                y: {
                    beginAtZero: true
                }
            }
        }
    });
}
`
