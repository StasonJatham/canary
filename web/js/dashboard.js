// Dashboard - Live Data Polling Only
// All write operations (logout, clear matches) are handled by HTML forms
class Dashboard {
    constructor() {
        this.matches = [];
        this.filteredMatches = [];
        this.currentPage = 0;
        this.pageSize = 40;
        this.refreshInterval = null;

        this.init();
    }

    async init() {
        this.setupEventListeners();
        // Theme toggle is now handled by base.html template

        // Initial load
        await this.loadMetrics();
        await this.loadMatches();
        await this.loadPerformanceMetrics();

        // Start auto-refresh for live data
        this.startAutoRefresh();
    }

    setupEventListeners() {
        // Search and filter (client-side)
        const refreshBtn = document.getElementById('refreshBtn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.loadMatches());
        }

        const searchInput = document.getElementById('searchInput');
        if (searchInput) {
            searchInput.addEventListener('input', () => this.filterMatches());
        }

        const timeRange = document.getElementById('timeRange');
        if (timeRange) {
            timeRange.addEventListener('change', () => this.loadMatches());
        }

        const priorityFilter = document.getElementById('priorityFilter');
        if (priorityFilter) {
            priorityFilter.addEventListener('change', () => this.filterMatches());
        }

        // Pagination
        const prevPage = document.getElementById('prevPage');
        if (prevPage) {
            prevPage.addEventListener('click', () => this.prevPage());
        }

        const nextPage = document.getElementById('nextPage');
        if (nextPage) {
            nextPage.addEventListener('click', () => this.nextPage());
        }

        // Clear matches button (shows confirmation)
        const clearBtn = document.getElementById('clearBtn');
        if (clearBtn) {
            clearBtn.addEventListener('click', () => {
                if (confirm('Are you sure you want to clear all matches from memory?')) {
                    const clearForm = document.getElementById('clearForm');
                    if (clearForm) {
                        clearForm.submit();
                    }
                }
            });
        }
    }


    // Helper to safely update element text content
    safeSetText(elementId, text) {
        const element = document.getElementById(elementId);
        if (element) {
            element.textContent = text;
        }
    }

    async loadMetrics() {
        try {
            const response = await fetch('/api/metrics');
            if (!response.ok) throw new Error('Failed to load metrics');

            const data = await response.json();
            this.safeSetText('totalMatches', data.total_matches.toLocaleString());
            this.safeSetText('totalCerts', data.total_certs.toLocaleString());
            this.safeSetText('activeRules', data.rules_count.toLocaleString());

            // Format uptime
            const uptime = data.uptime_seconds;
            let uptimeStr = '';
            if (uptime < 60) {
                uptimeStr = uptime + 's';
            } else if (uptime < 3600) {
                uptimeStr = Math.floor(uptime / 60) + 'm';
            } else if (uptime < 86400) {
                uptimeStr = Math.floor(uptime / 3600) + 'h';
            } else {
                uptimeStr = Math.floor(uptime / 86400) + 'd';
            }
            this.safeSetText('uptime', uptimeStr);

            // Show clear button if there are matches
            const clearBtn = document.getElementById('clearBtn');
            if (clearBtn && data.recent_matches > 0) {
                clearBtn.style.display = '';
            }

            this.updateStatusBadge(true);
        } catch (error) {
            console.error('Error loading metrics:', error);
            this.updateStatusBadge(false);
        }
    }

    async loadPerformanceMetrics() {
        try {
            const response = await fetch('/api/metrics/performance?minutes=60');
            if (!response.ok) throw new Error('Failed to load performance metrics');

            const data = await response.json();
            const current = data.current;

            if (current) {
                this.safeSetText('certsPerMin', current.certs_per_minute.toLocaleString());
                this.safeSetText('matchesPerMin', current.matches_per_minute.toLocaleString());
                this.safeSetText('avgMatchTime', current.avg_match_time_us + ' μs');
                this.safeSetText('cpuUsage', current.cpu_percent.toFixed(1) + '%');
                this.safeSetText('memoryUsage', current.memory_used_mb.toFixed(1) + ' MB');
                this.safeSetText('goroutines', current.goroutine_count.toLocaleString());
            }
        } catch (error) {
            console.error('Error loading performance metrics:', error);
        }
    }

    async loadMatches() {
        const timeRangeEl = document.getElementById('timeRange');
        const timeRange = timeRangeEl ? timeRangeEl.value : '30';

        try {
            const response = await fetch(`/api/matches/recent?minutes=${timeRange}`);
            if (!response.ok) throw new Error('Failed to load matches');

            const data = await response.json();
            this.matches = data.matches || [];
            this.matches = this.sortByNewestFirst(this.matches);

            this.filterMatches();
            this.updateStatusBadge(true);
        } catch (error) {
            console.error('Error loading matches:', error);
            this.updateStatusBadge(false);
            this.matches = [];
            this.renderMatches();
        }
    }

    sortByNewestFirst(matches) {
        return matches.sort((a, b) => {
            const dateA = new Date(a.detected_at);
            const dateB = new Date(b.detected_at);
            return dateB - dateA;
        });
    }

    filterMatches() {
        const searchInputEl = document.getElementById('searchInput');
        const priorityFilterEl = document.getElementById('priorityFilter');

        const searchTerm = searchInputEl ? searchInputEl.value.toLowerCase() : '';
        const priorityFilter = priorityFilterEl ? priorityFilterEl.value : '';

        this.filteredMatches = this.matches.filter(match => {
            const domainMatch = match.dns_names.some(domain =>
                domain.toLowerCase().includes(searchTerm)
            );
            const priorityMatch = !priorityFilter || match.priority === priorityFilter;
            return domainMatch && priorityMatch;
        });

        this.currentPage = 0;
        this.renderMatches();
    }

    renderMatches() {
        const tbody = document.getElementById('matchesTableBody');
        if (!tbody) return;

        // Capture currently open rows to restore state after refresh
        const openRows = new Set();
        tbody.querySelectorAll('.collapse.show').forEach(el => {
            openRows.add(el.id);
        });

        const start = this.currentPage * this.pageSize;
        const end = start + this.pageSize;
        const pageMatches = this.filteredMatches.slice(start, end);

        // Update counts
        this.safeSetText('matchCount', `${this.filteredMatches.length} matches`);
        this.safeSetText('matchCountFooter', `${this.filteredMatches.length} matches`);

        // Update pagination buttons
        const prevPage = document.getElementById('prevPage');
        const nextPage = document.getElementById('nextPage');
        if (prevPage) prevPage.disabled = this.currentPage === 0;
        if (nextPage) nextPage.disabled = end >= this.filteredMatches.length;

        if (pageMatches.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="5" class="text-center text-muted py-5">
                        <i class="bi bi-inbox fs-1 d-block mb-2"></i>
                        No matches found. Adjust filters or wait for new certificates...
                    </td>
                </tr>
            `;
            return;
        }

        tbody.innerHTML = pageMatches.map(match => this.renderMatchRow(match, openRows)).join('');
    }

    renderMatchRow(match, openRows = new Set()) {
        const timestamp = new Date(match.detected_at).toLocaleString();
        // Use stable ID based on hash to preserve state across refreshes
        const matchId = `match-${match.tbs_sha256 || match.id || Math.random().toString(36).substr(2, 9)}`;
        const isOpen = openRows.has(matchId);
        
        // Process keywords first to use for domain highlighting
        let keywords = [];
        if (Array.isArray(match.matched_domains)) {
            match.matched_domains.forEach(s => {
                if (typeof s === 'string') {
                    s.split(',').forEach(k => {
                        if (k.trim()) keywords.push(k.trim());
                    });
                }
            });
        } else if (typeof match.matched_domains === 'string') {
            keywords = match.matched_domains.split(',').filter(k => k.trim());
        }
        // Deduplicate keywords
        keywords = [...new Set(keywords)];

        // Helper for highlighting
        const escapeRegExp = (string) => string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const highlightDomain = (domain) => {
            if (!keywords.length) return this.escapeHtml(domain);
            const pattern = new RegExp(`(${keywords.map(k => escapeRegExp(k)).join('|')})`, 'gi');
            return domain.split(pattern).map(part => {
                return keywords.some(k => k.toLowerCase() === part.toLowerCase())
                    ? `<span class="text-danger fw-bold">${this.escapeHtml(part)}</span>`
                    : this.escapeHtml(part);
            }).join('');
        };

        // Sort domains: matches first
        const isMatch = (domain) => keywords.some(kw => domain.toLowerCase().includes(kw.toLowerCase()));
        const sortedDomains = [...match.dns_names].sort((a, b) => {
            const aMatch = isMatch(a);
            const bMatch = isMatch(b);
            if (aMatch && !bMatch) return -1;
            if (!aMatch && bMatch) return 1;
            return a.localeCompare(b);
        });

        const domainCount = sortedDomains.length;
        const firstDomain = sortedDomains[0];
        const remainingCount = domainCount - 1;

        const priorityBadge = {
            critical: 'danger',
            high: 'warning',
            medium: 'info',
            low: 'secondary'
        }[match.priority] || 'secondary';

        const keywordsHtml = keywords.map(k => 
            `<span class="badge bg-light text-dark border me-1 mb-1">${this.escapeHtml(k)}</span>`
        ).join('');

        // Main Row
        const mainRow = `
            <tr style="cursor: pointer;" data-bs-toggle="collapse" data-bs-target="#${matchId}" aria-expanded="${isOpen}" aria-controls="${matchId}" class="${isOpen ? '' : 'collapsed'}">
                <td class="text-nowrap d-none d-md-table-cell"><small>${this.escapeHtml(timestamp)}</small></td>
                <td>
                    <div class="d-flex align-items-center">
                        <i class="bi bi-chevron-down me-2 text-muted" style="font-size: 0.8em;"></i>
                        <span class="text-truncate" style="max-width: 200px;">
                            ${highlightDomain(firstDomain)}
                        </span>
                        ${remainingCount > 0 ? `<span class="badge bg-secondary ms-2">+${remainingCount}</span>` : ''}
                    </div>
                    <div class="d-md-none mt-1">
                        <small class="text-muted">${this.escapeHtml(timestamp)}</small>
                    </div>
                </td>
                <td class="d-none d-sm-table-cell"><span class="badge bg-secondary">${this.escapeHtml(match.matched_rule)}</span></td>
                <td><span class="badge bg-${priorityBadge}">${this.escapeHtml(match.priority)}</span></td>
                <td class="d-none d-md-table-cell">${keywordsHtml}</td>
            </tr>
        `;

        // Details Row (Collapsible)
        const detailsRow = `
            <tr>
                <td colspan="5" class="p-0 border-0">
                    <div class="collapse ${isOpen ? 'show' : ''}" id="${matchId}">
                        <div class="card card-body bg-light m-2 border-0">
                            <div class="d-sm-none mb-2">
                                <strong>Rule:</strong> ${this.escapeHtml(match.matched_rule)}<br>
                                <strong>Keywords:</strong> ${keywordsHtml}
                            </div>
                            <h6 class="card-subtitle mb-2 text-muted">All Domains (${domainCount})</h6>
                            <div class="row g-2">
                                ${sortedDomains.map(domain => {
                                    return `
                                    <div class="col-12 col-md-6 col-lg-4">
                                        <div class="d-flex align-items-center p-2 rounded bg-white border" 
                                             role="button"
                                             onclick="window.copyToClipboard('${this.escapeHtml(domain)}', this.querySelector('.copy-icon-wrapper'))">
                                            <span class="text-truncate me-2 flex-grow-1 text-muted" title="${this.escapeHtml(domain)}">
                                                ${highlightDomain(domain)}
                                            </span>
                                            <span class="copy-icon-wrapper text-muted" style="cursor: pointer;">
                                                <i class="bi bi-clipboard"></i>
                                            </span>
                                        </div>
                                    </div>`;
                                }).join('')}
                            </div>
                        </div>
                    </div>
                </td>
            </tr>
        `;

        return mainRow + detailsRow;
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
        // Refresh metrics and matches every 5 seconds
        this.refreshInterval = setInterval(() => {
            this.loadMetrics();
            this.loadMatches();
            this.loadPerformanceMetrics();
        }, 5000);
    }

    updateStatusBadge(online) {
        const badge = document.getElementById('statusBadge');
        if (!badge) return;

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
}

// Global copy to clipboard function
window.copyToClipboard = function(text, element) {
    navigator.clipboard.writeText(text).then(() => {
        // Show feedback
        const originalHtml = element.innerHTML;
        element.innerHTML = '<i class="bi bi-check2 me-1 text-success"></i>Copied!';
        setTimeout(() => {
            element.innerHTML = originalHtml;
        }, 1500);
    }).catch(err => {
        console.error('Failed to copy:', err);
    });
};

// Initialize dashboard when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        const dashboard = new Dashboard();
    });
} else {
    // DOM already loaded
    const dashboard = new Dashboard();
}
