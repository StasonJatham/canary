// Dashboard functionality
class Dashboard {
    constructor() {
        this.matches = [];
        this.filteredMatches = [];
        this.currentPage = 0;
        this.pageSize = 20;
        this.refreshInterval = null;
        this.publicDashboard = false;
        this.authenticated = false;

        this.init();
    }

    // Single ordering function - always sort by newest timestamp first
    sortByNewestFirst(matches) {
        return matches.sort((a, b) => {
            const dateA = new Date(a.detected_at);
            const dateB = new Date(b.detected_at);
            return dateB - dateA; // Descending order (newest first)
        });
    }

    async init() {
        // Wait for auth check to complete before loading data
        await this.checkPublicMode();

        this.setupEventListeners();
        this.setupThemeToggle();

        // Now load data with correct auth state
        await this.loadMetrics();
        await this.loadMatches();
        await this.loadPerformanceMetrics();
        this.startAutoRefresh();
    }

    async checkPublicMode() {
        try {
            const response = await fetch('/config', {
                credentials: 'same-origin'
            });
            if (response.ok) {
                const data = await response.json();
                this.publicDashboard = data.public_dashboard || false;
                this.authenticated = data.authenticated || false;

                const clearBtn = document.getElementById('clearBtn');

                // Show button if: NOT in public mode OR authenticated
                // Hide button if: in public mode AND not authenticated
                const shouldShowButton = !this.publicDashboard || this.authenticated;

                if (clearBtn) {
                    clearBtn.style.display = shouldShowButton ? '' : 'none';
                }
            }
        } catch (error) {
            console.error('Error checking public mode:', error);
            // On error, keep button hidden for security
        }
    }

    setupEventListeners() {
        document.getElementById('refreshBtn').addEventListener('click', () => this.loadMatches());
        document.getElementById('clearBtn').addEventListener('click', () => this.clearMatches());
        document.getElementById('searchInput').addEventListener('input', () => this.filterMatches());
        document.getElementById('timeRange').addEventListener('change', () => this.loadMatches());
        document.getElementById('priorityFilter').addEventListener('change', () => this.filterMatches());
        document.getElementById('prevPage').addEventListener('click', () => this.prevPage());
        document.getElementById('nextPage').addEventListener('click', () => this.nextPage());
        document.getElementById('logoutBtn').addEventListener('click', () => this.logout());
    }

    async loadPerformanceMetrics() {
        try {
            const response = await fetch('/metrics/performance?minutes=60');
            if (!response.ok) throw new Error('Failed to load performance metrics');

            const data = await response.json();
            const current = data.current;

            if (current) {
                document.getElementById('certsPerMin').textContent = current.certs_per_minute.toLocaleString();
                document.getElementById('matchesPerMin').textContent = current.matches_per_minute.toLocaleString();
                document.getElementById('avgMatchTime').textContent = current.avg_match_time_us + ' μs';
                document.getElementById('cpuUsage').textContent = current.cpu_percent.toFixed(1) + '%';
                document.getElementById('memoryUsage').textContent = current.memory_used_mb.toFixed(1) + ' MB';
                document.getElementById('goroutines').textContent = current.goroutine_count.toLocaleString();
            }
        } catch (error) {
            console.error('Error loading performance metrics:', error);
        }
    }

    setupThemeToggle() {
        const themeToggle = document.getElementById('themeToggle');
        const html = document.documentElement;

        // Check saved theme
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

    async loadMetrics() {
        try {
            const response = await fetch('/metrics');
            if (!response.ok) throw new Error('Failed to load metrics');

            const data = await response.json();

            document.getElementById('totalMatches').textContent = data.total_matches.toLocaleString();
            document.getElementById('totalCerts').textContent = data.total_certs.toLocaleString();
            document.getElementById('uptime').textContent = this.formatUptime(data.uptime_seconds);

            // Load rules count
            const rulesResponse = await fetch('/rules');
            if (rulesResponse.ok) {
                const rulesData = await rulesResponse.json();
                const activeRules = rulesData.rules.filter(r => r.enabled).length;
                document.getElementById('activeRules').textContent = activeRules;
            }

            this.updateStatusBadge(true);
        } catch (error) {
            console.error('Error loading metrics:', error);
            this.updateStatusBadge(false);
        }
    }

    formatUptime(seconds) {
        const days = Math.floor(seconds / 86400);
        const hours = Math.floor((seconds % 86400) / 3600);
        const minutes = Math.floor((seconds % 3600) / 60);

        if (days > 0) return `${days}d ${hours}h`;
        if (hours > 0) return `${hours}h ${minutes}m`;
        return `${minutes}m`;
    }

    updateStatusBadge(online) {
        const badge = document.getElementById('statusBadge');
        if (badge) {
            if (online) {
                badge.className = 'badge bg-success';
                badge.innerHTML = '<i class="bi bi-check-circle me-1"></i>Online';
            } else {
                badge.className = 'badge bg-danger';
                badge.innerHTML = '<i class="bi bi-x-circle me-1"></i>Offline';
            }
        }
    }

    async loadMatches() {
        const minutes = document.getElementById('timeRange').value;

        try {
            const response = await fetch(`/matches/recent?minutes=${minutes}`);
            if (!response.ok) throw new Error('Failed to load matches');

            const data = await response.json();
            this.matches = data.matches || [];

            // Always sort by newest first using the single ordering function
            this.sortByNewestFirst(this.matches);

            this.filterMatches();
            this.loadMetrics(); // Refresh metrics too
        } catch (error) {
            console.error('Error loading matches:', error);
            this.showToast('Failed to load matches', 'danger');
        }
    }

    filterMatches() {
        const searchTerm = document.getElementById('searchInput').value.toLowerCase();
        const priority = document.getElementById('priorityFilter').value;

        this.filteredMatches = this.matches.filter(match => {
            const matchesSearch = !searchTerm ||
                match.dns_names.some(d => d.toLowerCase().includes(searchTerm)) ||
                match.matched_rule.toLowerCase().includes(searchTerm);

            const matchesPriority = !priority || match.priority === priority;

            return matchesSearch && matchesPriority;
        });

        // Always sort by newest first using the single ordering function
        this.sortByNewestFirst(this.filteredMatches);

        this.currentPage = 0;
        this.renderMatches();
    }

    renderMatches() {
        const tbody = document.getElementById('matchesTableBody');
        const start = this.currentPage * this.pageSize;
        const end = start + this.pageSize;
        const pageMatches = this.filteredMatches.slice(start, end);

        if (pageMatches.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="text-center text-muted py-5">
                        <i class="bi bi-inbox fs-1 d-block mb-2"></i>
                        ${this.matches.length === 0 ? 'No matches found. Waiting for certificates...' : 'No matches found for the current filters.'}
                    </td>
                </tr>
            `;
        } else {
            tbody.innerHTML = pageMatches.map(match => this.renderMatchRow(match)).join('');
        }

        // Update pagination
        document.getElementById('matchCount').textContent =
            `${this.filteredMatches.length} match${this.filteredMatches.length !== 1 ? 'es' : ''}`;

        document.getElementById('prevPage').disabled = this.currentPage === 0;
        document.getElementById('nextPage').disabled = end >= this.filteredMatches.length;
    }

    renderMatchRow(match) {
        const timestamp = new Date(match.detected_at).toLocaleString();
        const domains = match.dns_names.slice(0, 3).join(', ');
        const moreCount = match.dns_names.length > 3 ? ` (+${match.dns_names.length - 3} more)` : '';
        const keywords = (match.matched_domains || []).join(', ');

        const priorityBadge = {
            critical: 'danger',
            high: 'warning',
            medium: 'info',
            low: 'secondary'
        }[match.priority] || 'secondary';

        return `
            <tr>
                <td><small class="text-muted">${timestamp}</small></td>
                <td>
                    <div class="text-truncate" style="max-width: 300px;" title="${match.dns_names.join(', ')}">
                        ${this.escapeHtml(domains)}${moreCount}
                    </div>
                </td>
                <td>
                    <span class="badge bg-dark">${this.escapeHtml(match.matched_rule || 'N/A')}</span>
                </td>
                <td>
                    <span class="badge bg-${priorityBadge}">${match.priority}</span>
                </td>
                <td>
                    <small class="text-muted">${this.highlightKeywords(keywords, match.matched_domains)}</small>
                </td>
                <td>
                    <button class="btn btn-sm btn-outline-secondary" onclick="dashboard.viewCertDetails('${match.tbs_sha256}')">
                        <i class="bi bi-eye"></i>
                    </button>
                </td>
            </tr>
        `;
    }

    highlightKeywords(text, keywords) {
        if (!keywords || keywords.length === 0) return this.escapeHtml(text);

        let highlighted = this.escapeHtml(text);
        keywords.forEach(keyword => {
            const escapedKeyword = this.escapeHtml(keyword).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
            const regex = new RegExp(`(${escapedKeyword})`, 'gi');
            highlighted = highlighted.replace(regex, '<mark class="bg-warning bg-opacity-50">$1</mark>');
        });
        return highlighted;
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    async viewCertDetails(tbsSha256) {
        const modal = new bootstrap.Modal(document.getElementById('certModal'));
        const modalBody = document.getElementById('certModalBody');

        modalBody.innerHTML = '<div class="text-center"><div class="spinner-border text-primary"></div></div>';
        modal.show();

        // Find the certificate details from matches
        const match = this.matches.find(m => m.tbs_sha256 === tbsSha256);
        if (match) {
            modalBody.innerHTML = `
                <div class="row g-3">
                    <div class="col-12">
                        <h6>DNS Names</h6>
                        <div class="alert alert-secondary mb-0">
                            ${match.dns_names.map(d => `<div>${this.escapeHtml(d)}</div>`).join('')}
                        </div>
                    </div>
                    <div class="col-md-6">
                        <h6>Matched Rule</h6>
                        <p>${this.escapeHtml(match.matched_rule || 'N/A')}</p>
                    </div>
                    <div class="col-md-6">
                        <h6>Priority</h6>
                        <p><span class="badge bg-secondary">${match.priority}</span></p>
                    </div>
                    <div class="col-12">
                        <h6>Keywords</h6>
                        <p>${(match.matched_domains || []).map(k => `<span class="badge bg-info me-1">${this.escapeHtml(k)}</span>`).join('')}</p>
                    </div>
                    <div class="col-md-6">
                        <h6>TBS SHA256</h6>
                        <p><code class="small">${tbsSha256}</code></p>
                    </div>
                    <div class="col-md-6">
                        <h6>Cert SHA256</h6>
                        <p><code class="small">${match.cert_sha256}</code></p>
                    </div>
                </div>
            `;
        }
    }

    async clearMatches() {
        if (!confirm('Are you sure you want to clear all matches from memory?')) return;

        try {
            const response = await fetch('/matches/clear', { method: 'POST' });
            if (!response.ok) throw new Error('Failed to clear matches');

            this.showToast('Matches cleared successfully', 'success');
            this.loadMatches();
        } catch (error) {
            console.error('Error clearing matches:', error);
            this.showToast('Failed to clear matches', 'danger');
        }
    }

    prevPage() {
        if (this.currentPage > 0) {
            this.currentPage--;
            this.renderMatches();
        }
    }

    nextPage() {
        const maxPage = Math.ceil(this.filteredMatches.length / this.pageSize) - 1;
        if (this.currentPage < maxPage) {
            this.currentPage++;
            this.renderMatches();
        }
    }

    startAutoRefresh() {
        // Refresh every 20 seconds
        this.refreshInterval = setInterval(() => {
            this.loadMatches();
            this.loadPerformanceMetrics();
        }, 20000);
    }

    showToast(message, type = 'info') {
        // Simple toast notification using Bootstrap alerts
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

// Initialize dashboard
const dashboard = new Dashboard();
