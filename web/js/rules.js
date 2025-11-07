// Rules management functionality
class RulesManager {
    constructor() {
        this.rules = [];
        this.currentRule = null;
        this.ruleModal = null;
        this.deleteModal = null;
        this.publicDashboard = false;

        this.init();
    }

    async init() {
        await this.checkPublicMode();
        this.ruleModal = new bootstrap.Modal(document.getElementById('ruleModal'));
        this.deleteModal = new bootstrap.Modal(document.getElementById('deleteModal'));

        this.setupEventListeners();
        this.setupThemeToggle();
        this.loadRules();
    }

    async checkPublicMode() {
        try {
            const response = await fetch('/config');
            if (response.ok) {
                const data = await response.json();
                this.publicDashboard = data.public_dashboard || false;

                // Hide action buttons if in public mode
                if (this.publicDashboard) {
                    const addRuleBtn = document.getElementById('addRuleBtn');
                    const reloadBtn = document.getElementById('reloadRulesBtn');
                    if (addRuleBtn) addRuleBtn.style.display = 'none';
                    if (reloadBtn) reloadBtn.style.display = 'none';
                }
            }
        } catch (error) {
            console.error('Error checking public mode:', error);
        }
    }

    setupEventListeners() {
        document.getElementById('addRuleBtn').addEventListener('click', () => this.showAddRule());
        document.getElementById('reloadRulesBtn').addEventListener('click', () => this.reloadFromYAML());
        document.getElementById('ruleForm').addEventListener('submit', (e) => this.handleSubmit(e));
        document.getElementById('confirmDeleteBtn').addEventListener('click', () => this.confirmDelete());
        document.getElementById('logoutBtn').addEventListener('click', () => this.logout());
    }

    setupThemeToggle() {
        const themeToggle = document.getElementById('themeToggle');
        const html = document.documentElement;

        const savedTheme = localStorage.getItem('theme') || 'light';
        html.setAttribute('data-bs-theme', savedTheme);
        this.updateThemeIcon(savedTheme);

        themeToggle.addEventListener('click', () => {
            const currentTheme = html.getAttribute('data-bs-theme');
            const newTheme = currentTheme === 'light' ? 'dark' : 'light';
            html.setAttribute('data-bs-theme', newTheme);
            localStorage.setItem('theme', newTheme);
            this.updateThemeIcon(newTheme);
        });
    }

    updateThemeIcon(theme) {
        const icon = document.querySelector('#themeToggle i');
        icon.className = theme === 'light' ? 'bi bi-moon-fill' : 'bi bi-sun-fill';
    }

    async loadRules() {
        try {
            const response = await fetch('/rules');
            if (!response.ok) throw new Error('Failed to load rules');

            const data = await response.json();
            this.rules = data.rules || [];
            this.renderRules();
            this.updateStatusBadge(true);
        } catch (error) {
            console.error('Error loading rules:', error);
            this.showToast('Failed to load rules', 'danger');
            this.updateStatusBadge(false);
        }
    }

    renderRules() {
        const tbody = document.getElementById('rulesTableBody');
        document.getElementById('rulesCount').textContent = this.rules.length;

        if (this.rules.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="text-center text-muted py-5">
                        <i class="bi bi-inbox fs-1 d-block mb-2"></i>
                        No rules found. Click "Add Rule" to create one.
                    </td>
                </tr>
            `;
            return;
        }

        tbody.innerHTML = this.rules.map(rule => this.renderRuleRow(rule)).join('');
    }

    renderRuleRow(rule) {
        const statusIcon = rule.enabled ?
            '<i class="bi bi-check-circle-fill text-success"></i>' :
            '<i class="bi bi-x-circle-fill text-muted"></i>';

        const priorityBadge = {
            critical: 'danger',
            high: 'warning',
            medium: 'info',
            low: 'secondary'
        }[rule.priority] || 'secondary';

        // Hide action buttons in public mode
        const actionsColumn = this.publicDashboard ? '' : `
            <td>
                <div class="btn-group btn-group-sm">
                    <button class="btn btn-outline-secondary" onclick="rulesManager.toggleRule('${rule.name}')" title="${rule.enabled ? 'Disable' : 'Enable'}">
                        <i class="bi ${rule.enabled ? 'bi-pause' : 'bi-play'}"></i>
                    </button>
                    <button class="btn btn-outline-primary" onclick="rulesManager.showEditRule('${rule.name}')" title="Edit">
                        <i class="bi bi-pencil"></i>
                    </button>
                    <button class="btn btn-outline-danger" onclick="rulesManager.showDeleteRule('${rule.name}')" title="Delete">
                        <i class="bi bi-trash"></i>
                    </button>
                </div>
            </td>
        `;

        return `
            <tr>
                <td class="text-center">${statusIcon}</td>
                <td>
                    <div class="fw-semibold">${this.escapeHtml(rule.name)}</div>
                    ${rule.comment ? `<small class="text-muted">${this.escapeHtml(rule.comment)}</small>` : ''}
                </td>
                <td>
                    <code class="small">${this.escapeHtml(rule.keywords)}</code>
                </td>
                <td>
                    <span class="badge bg-${priorityBadge}">${rule.priority}</span>
                </td>
                <td class="text-center">
                    <span class="badge bg-secondary">${rule.order}</span>
                </td>
                ${actionsColumn}
            </tr>
        `;
    }

    showAddRule() {
        this.currentRule = null;
        document.getElementById('ruleModalTitle').textContent = 'Add Rule';
        document.getElementById('ruleForm').reset();
        this.ruleModal.show();
    }

    showEditRule(ruleName) {
        const rule = this.rules.find(r => r.name === ruleName);
        if (!rule) return;

        this.currentRule = rule;
        document.getElementById('ruleModalTitle').textContent = 'Edit Rule';
        document.getElementById('ruleName').value = rule.name;
        document.getElementById('ruleExpression').value = rule.keywords;
        document.getElementById('rulePriority').value = rule.priority;
        document.getElementById('ruleEnabled').value = rule.enabled.toString();
        document.getElementById('ruleComment').value = rule.comment || '';

        // Disable name editing for existing rules
        document.getElementById('ruleName').readOnly = true;

        this.ruleModal.show();
    }

    async handleSubmit(e) {
        e.preventDefault();

        const name = document.getElementById('ruleName').value.trim();
        const keywords = document.getElementById('ruleExpression').value.trim();
        const priority = document.getElementById('rulePriority').value;
        const enabled = document.getElementById('ruleEnabled').value === 'true';
        const comment = document.getElementById('ruleComment').value.trim();

        const ruleData = { name, keywords, priority, enabled, comment };

        try {
            let response;
            if (this.currentRule) {
                // Update existing rule
                response = await fetch(`/rules/update/${name}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(ruleData)
                });
            } else {
                // Create new rule
                response = await fetch('/rules/create', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(ruleData)
                });
            }

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Failed to save rule');
            }

            this.showToast(`Rule ${this.currentRule ? 'updated' : 'created'} successfully`, 'success');
            this.ruleModal.hide();
            this.loadRules();

            // Re-enable name field
            document.getElementById('ruleName').readOnly = false;
        } catch (error) {
            console.error('Error saving rule:', error);
            this.showToast(error.message, 'danger');
        }
    }

    async toggleRule(ruleName) {
        const rule = this.rules.find(r => r.name === ruleName);
        if (!rule) return;

        try {
            const response = await fetch(`/rules/toggle/${ruleName}`, {
                method: 'PUT'
            });

            if (!response.ok) throw new Error('Failed to toggle rule');

            this.showToast(`Rule ${rule.enabled ? 'disabled' : 'enabled'} successfully`, 'success');
            this.loadRules();
        } catch (error) {
            console.error('Error toggling rule:', error);
            this.showToast('Failed to toggle rule', 'danger');
        }
    }

    showDeleteRule(ruleName) {
        this.currentRule = this.rules.find(r => r.name === ruleName);
        document.getElementById('deleteRuleName').textContent = ruleName;
        this.deleteModal.show();
    }

    async confirmDelete() {
        if (!this.currentRule) return;

        try {
            const response = await fetch(`/rules/delete/${this.currentRule.name}`, {
                method: 'DELETE'
            });

            if (!response.ok) throw new Error('Failed to delete rule');

            this.showToast('Rule deleted successfully', 'success');
            this.deleteModal.hide();
            this.loadRules();
        } catch (error) {
            console.error('Error deleting rule:', error);
            this.showToast('Failed to delete rule', 'danger');
        }
    }

    async reloadFromYAML() {
        if (!confirm('This will reload all rules from the rules.yaml file. Any unsaved changes will be lost. Continue?')) {
            return;
        }

        try {
            const response = await fetch('/rules/reload', {
                method: 'POST'
            });

            if (!response.ok) throw new Error('Failed to reload rules');

            this.showToast('Rules reloaded from YAML successfully', 'success');
            this.loadRules();
        } catch (error) {
            console.error('Error reloading rules:', error);
            this.showToast('Failed to reload rules from YAML', 'danger');
        }
    }

    updateStatusBadge(online) {
        const badge = document.getElementById('statusBadge');
        if (online) {
            badge.className = 'badge bg-success';
            badge.innerHTML = '<i class="bi bi-check-circle me-1"></i>Online';
        } else {
            badge.className = 'badge bg-danger';
            badge.innerHTML = '<i class="bi bi-x-circle me-1"></i>Offline';
        }
    }

    escapeHtml(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    showToast(message, type = 'info') {
        const alertDiv = document.createElement('div');
        alertDiv.className = `alert alert-${type} alert-dismissible fade show position-fixed top-0 end-0 m-3`;
        alertDiv.style.zIndex = '9999';
        alertDiv.innerHTML = `
            ${message}
            <button type="button" class="btn-close" data-bs-dismiss="alert"></button>
        `;
        document.body.appendChild(alertDiv);

        setTimeout(() => {
            alertDiv.remove();
        }, 5000);
    }

    async logout() {
        try {
            const response = await fetch('/auth/logout', { method: 'POST' });
            if (response.ok) {
                window.location.href = '/login';
            } else {
                this.showToast('Failed to logout', 'danger');
            }
        } catch (error) {
            console.error('Error logging out:', error);
            this.showToast('Failed to logout', 'danger');
        }
    }
}

// Initialize rules manager
const rulesManager = new RulesManager();
