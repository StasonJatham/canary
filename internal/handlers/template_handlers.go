package handlers

import (
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"canary/internal/auth"
	"canary/internal/config"
	"canary/internal/rules"
)

var templates *template.Template

// InitTemplates loads all HTML templates
func InitTemplates() error {
	var err error
	templates, err = template.ParseGlob("web/templates/*.html")
	if err != nil {
		return err
	}
	return nil
}

// parsePageTemplate parses a specific page template along with base.html
// This avoids namespace conflicts when multiple templates define the same blocks
func parsePageTemplate(templateFile string) (*template.Template, error) {
	return template.ParseFiles("web/templates/base.html", templateFile)
}

// getUserFromRequest extracts user info from session cookie
func getUserFromRequest(r *http.Request) (*auth.Session, bool) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return nil, false
	}

	session, err := auth.GetSessionByToken(config.DB, cookie.Value)
	if err != nil {
		return nil, false
	}

	return session, true
}

// getCSRFToken gets or creates a CSRF token for the current session
func getCSRFToken(r *http.Request) string {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return ""
	}

	token, err := auth.GetOrCreateCSRFToken(cookie.Value)
	if err != nil {
		return ""
	}

	return token
}

// canUserEdit determines if user can edit based on public dashboard mode and authentication
func canUserEdit(r *http.Request) bool {
	// If not in public dashboard mode, must be authenticated (enforced by middleware)
	if !config.PublicDashboard {
		return true // User reached here, so they're authenticated
	}

	// In public dashboard mode, must be authenticated to edit
	_, authenticated := getUserFromRequest(r)
	return authenticated
}

// ServeRulesPage renders the rules page with server-side data
func ServeRulesPage(w http.ResponseWriter, r *http.Request) {
	// Parse the rules template separately to avoid namespace conflicts
	tmpl, err := parsePageTemplate("web/templates/rules.html")
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get rules from engine
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		http.Error(w, "Rules engine not initialized", http.StatusInternalServerError)
		return
	}
	engine := engineVal.(*rules.Engine)

	// Check if user can edit
	canEdit := canUserEdit(r)

	// Get message from query params (for redirects with messages)
	message := r.URL.Query().Get("message")
	messageType := r.URL.Query().Get("type")
	if messageType == "" {
		messageType = "info"
	}

	data := struct {
		ActivePage  string
		Rules       []*rules.Rule
		CanEdit     bool
		Message     string
		MessageType string
		CSRFToken   string
	}{
		ActivePage:  "rules",
		Rules:       engine.Rules,
		CanEdit:     canEdit,
		Message:     message,
		MessageType: messageType,
		CSRFToken:   getCSRFToken(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "rules.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServeRuleForm renders the create/edit form
func ServeRuleForm(w http.ResponseWriter, r *http.Request) {
	// Parse the rule form template separately to avoid namespace conflicts
	tmpl, err := parsePageTemplate("web/templates/rule_form.html")
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract rule name from path if editing
	path := r.URL.Path
	var ruleName string
	var rule *rules.Rule

	if strings.HasPrefix(path, "/rules/edit/") {
		ruleName = strings.TrimPrefix(path, "/rules/edit/")

		// Get the rule
		engineVal := config.RuleEngine.Load()
		if engineVal == nil {
			http.Error(w, "Rules engine not initialized", http.StatusInternalServerError)
			return
		}
		engine := engineVal.(*rules.Engine)

		for i := range engine.Rules {
			if engine.Rules[i].Name == ruleName {
				rule = engine.Rules[i]
				break
			}
		}

		if rule == nil {
			http.Error(w, "Rule not found", http.StatusNotFound)
			return
		}
	}

	// Get error from query params (for form validation errors)
	errorMsg := r.URL.Query().Get("error")

	data := struct {
		Rule       *rules.Rule
		Error      string
		CSRFToken  string
		ActivePage string
	}{
		Rule:       rule,
		Error:      errorMsg,
		CSRFToken:  getCSRFToken(r),
		ActivePage: "rules",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "rule_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// CreateRuleForm handles rule creation form submission
func CreateRuleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/rules/new?error=Invalid form data", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	keywords := strings.TrimSpace(r.FormValue("keywords"))
	priority := r.FormValue("priority")
	enabled := r.FormValue("enabled") == "true"
	comment := strings.TrimSpace(r.FormValue("comment"))

	// Validate
	if name == "" || keywords == "" || priority == "" {
		http.Redirect(w, r, "/rules/new?error=Missing required fields", http.StatusSeeOther)
		return
	}

	// Save to rules file
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		http.Redirect(w, r, "/rules/new?error=Rules engine not initialized", http.StatusSeeOther)
		return
	}
	engine := engineVal.(*rules.Engine)

	// Check if rule already exists
	for _, rule := range engine.Rules {
		if rule.Name == name {
			http.Redirect(w, r, "/rules/new?error=Rule with this name already exists", http.StatusSeeOther)
			return
		}
	}

	// Add the rule
	newRule := rules.Rule{
		Name:     name,
		Keywords: keywords,
		Priority: rules.Priority(priority),
		Enabled:  enabled,
		Comment:  comment,
		Order:    len(engine.Rules) + 1,
	}

	// Load current YAML, add rule, save
	if err := saveRuleToFile(newRule, false); err != nil {
		http.Redirect(w, r, "/rules/new?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Redirect to rules page with success message
	http.Redirect(w, r, "/rules?message=Rule created successfully&type=success", http.StatusSeeOther)
}

// UpdateRuleForm handles rule update form submission
func UpdateRuleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract rule name from path
	ruleName := strings.TrimPrefix(r.URL.Path, "/rules/update/")
	if ruleName == "" {
		http.Error(w, "Rule name required", http.StatusBadRequest)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/rules/edit/"+ruleName+"?error=Invalid form data", http.StatusSeeOther)
		return
	}

	keywords := strings.TrimSpace(r.FormValue("keywords"))
	priority := r.FormValue("priority")
	enabled := r.FormValue("enabled") == "true"
	comment := strings.TrimSpace(r.FormValue("comment"))

	// Validate
	if keywords == "" || priority == "" {
		http.Redirect(w, r, "/rules/edit/"+ruleName+"?error=Missing required fields", http.StatusSeeOther)
		return
	}

	// Update rule
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		http.Redirect(w, r, "/rules/edit/"+ruleName+"?error=Rules engine not initialized", http.StatusSeeOther)
		return
	}
	engine := engineVal.(*rules.Engine)

	// Find the rule
	var ruleIndex = -1
	for i := range engine.Rules {
		if engine.Rules[i].Name == ruleName {
			ruleIndex = i
			break
		}
	}

	if ruleIndex == -1 {
		http.Redirect(w, r, "/rules?message=Rule not found&type=danger", http.StatusSeeOther)
		return
	}

	// Update rule fields
	updatedRule := *engine.Rules[ruleIndex]
	updatedRule.Keywords = keywords
	updatedRule.Priority = rules.Priority(priority)
	updatedRule.Enabled = enabled
	updatedRule.Comment = comment

	// Save to file
	if err := saveRuleToFile(updatedRule, true); err != nil {
		http.Redirect(w, r, "/rules/edit/"+ruleName+"?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Redirect with success message
	http.Redirect(w, r, "/rules?message=Rule updated successfully&type=success", http.StatusSeeOther)
}

// ToggleRuleForm handles rule toggle form submission
func ToggleRuleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract rule name from path
	ruleName := strings.TrimPrefix(r.URL.Path, "/rules/toggle/")
	if ruleName == "" {
		http.Error(w, "Rule name required", http.StatusBadRequest)
		return
	}

	// Call the existing ToggleRule handler logic
	engineVal := config.RuleEngine.Load()
	if engineVal == nil {
		http.Redirect(w, r, "/rules?message=Rules engine not initialized&type=danger", http.StatusSeeOther)
		return
	}
	engine := engineVal.(*rules.Engine)

	// Find and toggle the rule
	var ruleIndex = -1
	for i := range engine.Rules {
		if engine.Rules[i].Name == ruleName {
			ruleIndex = i
			break
		}
	}

	if ruleIndex == -1 {
		http.Redirect(w, r, "/rules?message=Rule not found&type=danger", http.StatusSeeOther)
		return
	}

	// Toggle enabled status
	updatedRule := *engine.Rules[ruleIndex]
	updatedRule.Enabled = !updatedRule.Enabled

	// Save to file
	if err := saveRuleToFile(updatedRule, true); err != nil {
		http.Redirect(w, r, "/rules?message="+err.Error()+"&type=danger", http.StatusSeeOther)
		return
	}

	action := "disabled"
	if updatedRule.Enabled {
		action = "enabled"
	}

	http.Redirect(w, r, "/rules?message=Rule "+action+" successfully&type=success", http.StatusSeeOther)
}

// DeleteRuleForm handles rule deletion form submission
func DeleteRuleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract rule name from path
	ruleName := strings.TrimPrefix(r.URL.Path, "/rules/delete/")
	if ruleName == "" {
		http.Error(w, "Rule name required", http.StatusBadRequest)
		return
	}

	// Delete rule from file
	if err := deleteRuleFromFile(ruleName); err != nil {
		http.Redirect(w, r, "/rules?message="+err.Error()+"&type=danger", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/rules?message=Rule deleted successfully&type=success", http.StatusSeeOther)
}

// ReloadRulesForm handles rules reload form submission
func ReloadRulesForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call existing reload logic
	ReloadRules(w, r)

	// If we got here without error, redirect with success
	http.Redirect(w, r, "/rules?message=Rules reloaded successfully&type=success", http.StatusSeeOther)
}

// saveRuleToFile saves a rule to the YAML file
func saveRuleToFile(rule rules.Rule, isUpdate bool) error {
	// Read current rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var newLines []string
	inRule := false
	ruleFound := false
	currentRuleName := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a rule name line
		if strings.HasPrefix(trimmed, "- name:") {
			currentRuleName = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
			currentRuleName = strings.Trim(currentRuleName, `"'`)

			if currentRuleName == rule.Name {
				ruleFound = true
				inRule = true

				if isUpdate {
					// Replace the rule
					newLines = append(newLines, "  - name: "+rule.Name)
					newLines = append(newLines, "    keywords: "+rule.Keywords)
					newLines = append(newLines, "    priority: "+string(rule.Priority))
					if rule.Enabled {
						newLines = append(newLines, "    enabled: true")
					} else {
						newLines = append(newLines, "    enabled: false")
					}
					if rule.Comment != "" {
						newLines = append(newLines, "    comment: "+rule.Comment)
					}
					continue
				}
			}
		}

		// Skip lines that are part of the rule being updated
		if inRule && currentRuleName == rule.Name && isUpdate {
			if strings.HasPrefix(trimmed, "- name:") || (trimmed != "" && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "  - ")) {
				inRule = false
				newLines = append(newLines, line)
			}
			continue
		}

		newLines = append(newLines, line)
	}

	// If creating new rule, append it
	if !isUpdate && !ruleFound {
		// Find the rules section and append
		for i := len(newLines) - 1; i >= 0; i-- {
			if strings.TrimSpace(newLines[i]) != "" {
				// Append after last non-empty line
				newRuleLines := []string{
					"  - name: " + rule.Name,
					"    keywords: " + rule.Keywords,
					"    priority: " + string(rule.Priority),
				}
				if rule.Enabled {
					newRuleLines = append(newRuleLines, "    enabled: true")
				} else {
					newRuleLines = append(newRuleLines, "    enabled: false")
				}
				if rule.Comment != "" {
					newRuleLines = append(newRuleLines, "    comment: "+rule.Comment)
				}

				newLines = append(newLines[:i+1], append(newRuleLines, newLines[i+1:]...)...)
				break
			}
		}
	}

	// Write back to file
	if err := os.WriteFile(config.RulesFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return err
	}

	// Reload rules
	newEngine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		return err
	}
	config.RuleEngine.Store(newEngine)
	return nil
}

// deleteRuleFromFile removes a rule from the YAML file
func deleteRuleFromFile(ruleName string) error {
	// Read current rules file
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var newLines []string
	inRule := false
	currentRuleName := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a rule name line
		if strings.HasPrefix(trimmed, "- name:") {
			currentRuleName = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
			currentRuleName = strings.Trim(currentRuleName, `"'`)

			if currentRuleName == ruleName {
				inRule = true
				continue
			}
		}

		// Skip lines that are part of the rule being deleted
		if inRule {
			if strings.HasPrefix(trimmed, "- name:") || (trimmed == "rules:") {
				// Hit next rule, stop skipping
				inRule = false
				newLines = append(newLines, line)
			}
			continue
		}

		newLines = append(newLines, line)
	}

	// Write back to file
	if err := os.WriteFile(config.RulesFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return err
	}

	// Reload rules
	newEngine, err := rules.LoadRules(config.RulesFile)
	if err != nil {
		return err
	}
	config.RuleEngine.Store(newEngine)
	return nil
}

// ServeDashboardPage renders the dashboard page with server-side data
func ServeDashboardPage(w http.ResponseWriter, r *http.Request) {
	// Parse the dashboard template separately to avoid namespace conflicts
	tmpl, err := parsePageTemplate("web/templates/dashboard.html")
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if user can edit
	canEdit := canUserEdit(r)

	// Get message from query params (for redirects with messages)
	message := r.URL.Query().Get("message")
	messageType := r.URL.Query().Get("type")
	if messageType == "" {
		messageType = "info"
	}

	// Get basic metrics
	uptime := time.Since(config.StartTime)

	data := struct {
		ActivePage  string
		CanEdit     bool
		Message     string
		MessageType string
		CSRFToken   string
		Uptime      int
	}{
		ActivePage:  "dashboard",
		CanEdit:     canEdit,
		Message:     message,
		MessageType: messageType,
		CSRFToken:   getCSRFToken(r),
		Uptime:      int(uptime.Seconds()),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServeLoginPage renders the login page
func ServeLoginPage(w http.ResponseWriter, r *http.Request) {
	// Parse the login template separately to avoid namespace conflicts
	tmpl, err := parsePageTemplate("web/templates/login.html")
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get error from query params (for login failures)
	errorMsg := r.URL.Query().Get("error")

	data := struct {
		Error string
	}{
		Error: errorMsg,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
