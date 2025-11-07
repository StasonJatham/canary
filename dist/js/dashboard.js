class Dashboard{constructor(){this.matches=[],this.filteredMatches=[],this.currentPage=0,this.pageSize=20,this.refreshInterval=null,this.publicDashboard=!1,this.authenticated=!1,this.init()}sortByNewestFirst(e){return e.sort((e,t)=>{const n=new Date(e.detected_at),s=new Date(t.detected_at);return s-n})}async init(){await this.checkPublicMode(),this.setupEventListeners(),this.setupThemeToggle(),await this.loadMetrics(),await this.loadMatches(),await this.loadPerformanceMetrics(),this.startAutoRefresh()}async checkPublicMode(){try{const e=await fetch("/config",{credentials:"same-origin"});if(e.ok){const t=await e.json();this.publicDashboard=t.public_dashboard||!1,this.authenticated=t.authenticated||!1;const n=document.getElementById("clearBtn"),s=!this.publicDashboard||this.authenticated;n&&(n.style.display=s?"":"none")}}catch(e){console.error("Error checking public mode:",e)}}setupEventListeners(){document.getElementById("refreshBtn").addEventListener("click",()=>this.loadMatches()),document.getElementById("clearBtn").addEventListener("click",()=>this.clearMatches()),document.getElementById("searchInput").addEventListener("input",()=>this.filterMatches()),document.getElementById("timeRange").addEventListener("change",()=>this.loadMatches()),document.getElementById("priorityFilter").addEventListener("change",()=>this.filterMatches()),document.getElementById("prevPage").addEventListener("click",()=>this.prevPage()),document.getElementById("nextPage").addEventListener("click",()=>this.nextPage()),document.getElementById("logoutBtn").addEventListener("click",()=>this.logout())}async loadPerformanceMetrics(){try{const t=await fetch("/metrics/performance?minutes=60");if(!t.ok)throw new Error("Failed to load performance metrics");const n=await t.json(),e=n.current;e&&(document.getElementById("certsPerMin").textContent=e.certs_per_minute.toLocaleString(),document.getElementById("matchesPerMin").textContent=e.matches_per_minute.toLocaleString(),document.getElementById("avgMatchTime").textContent=e.avg_match_time_us+" μs",document.getElementById("cpuUsage").textContent=e.cpu_percent.toFixed(1)+"%",document.getElementById("memoryUsage").textContent=e.memory_used_mb.toFixed(1)+" MB",document.getElementById("goroutines").textContent=e.goroutine_count.toLocaleString())}catch(e){console.error("Error loading performance metrics:",e)}}setupThemeToggle(){const n=document.getElementById("themeToggle"),e=document.documentElement,t=localStorage.getItem("theme")||"light";e.setAttribute("data-bs-theme",t),this.updateThemeIcon(t),n.addEventListener("click",()=>{const n=e.getAttribute("data-bs-theme"),t=n==="light"?"dark":"light";e.setAttribute("data-bs-theme",t),localStorage.setItem("theme",t),this.updateThemeIcon(t)})}updateThemeIcon(e){const t=document.querySelector("#themeToggle i");t.className=e==="light"?"bi bi-moon-fill":"bi bi-sun-fill"}async loadMetrics(){try{const t=await fetch("/metrics");if(!t.ok)throw new Error("Failed to load metrics");const e=await t.json();document.getElementById("totalMatches").textContent=e.total_matches.toLocaleString(),document.getElementById("totalCerts").textContent=e.total_certs.toLocaleString(),document.getElementById("uptime").textContent=this.formatUptime(e.uptime_seconds);const n=await fetch("/rules");if(n.ok){const e=await n.json(),t=e.rules.filter(e=>e.enabled).length;document.getElementById("activeRules").textContent=t}this.updateStatusBadge(!0)}catch(e){console.error("Error loading metrics:",e),this.updateStatusBadge(!1)}}formatUptime(e){const n=Math.floor(e/86400),t=Math.floor(e%86400/3600),s=Math.floor(e%3600/60);return n>0?`${n}d ${t}h`:t>0?`${t}h ${s}m`:`${s}m`}updateStatusBadge(e){const t=document.getElementById("statusBadge");t&&(e?(t.className="badge bg-success",t.innerHTML='<i class="bi bi-check-circle me-1"></i>Online'):(t.className="badge bg-danger",t.innerHTML='<i class="bi bi-x-circle me-1"></i>Offline'))}async loadMatches(){const e=document.getElementById("timeRange").value;try{const t=await fetch(`/matches/recent?minutes=${e}`);if(!t.ok)throw new Error("Failed to load matches");const n=await t.json();this.matches=n.matches||[],this.sortByNewestFirst(this.matches),this.filterMatches(),this.loadMetrics()}catch(e){console.error("Error loading matches:",e),this.showToast("Failed to load matches","danger")}}filterMatches(){const e=document.getElementById("searchInput").value.toLowerCase(),t=document.getElementById("priorityFilter").value;this.filteredMatches=this.matches.filter(n=>{const s=!e||n.dns_names.some(t=>t.toLowerCase().includes(e))||n.matched_rule.toLowerCase().includes(e),o=!t||n.priority===t;return s&&o}),this.sortByNewestFirst(this.filteredMatches),this.currentPage=0,this.renderMatches()}renderMatches(){const e=document.getElementById("matchesTableBody"),t=this.currentPage*this.pageSize,n=t+this.pageSize,s=this.filteredMatches.slice(t,n);s.length===0?e.innerHTML=`
                <tr>
                    <td colspan="6" class="text-center text-muted py-5">
                        <i class="bi bi-inbox fs-1 d-block mb-2"></i>
                        ${this.matches.length===0?"No matches found. Waiting for certificates...":"No matches found for the current filters."}
                    </td>
                </tr>
            `:e.innerHTML=s.map(e=>this.renderMatchRow(e)).join(""),document.getElementById("matchCount").textContent=`${this.filteredMatches.length} match${this.filteredMatches.length!==1?"es":""}`,document.getElementById("prevPage").disabled=this.currentPage===0,document.getElementById("nextPage").disabled=n>=this.filteredMatches.length}renderMatchRow(e){const t=new Date(e.detected_at).toLocaleString(),n=e.dns_names.slice(0,3).join(", "),s=e.dns_names.length>3?` (+${e.dns_names.length-3} more)`:"",o=(e.matched_domains||[]).join(", "),i={critical:"danger",high:"warning",medium:"info",low:"secondary"}[e.priority]||"secondary";return`
            <tr>
                <td><small class="text-muted">${t}</small></td>
                <td>
                    <div class="text-truncate" style="max-width: 300px;" title="${e.dns_names.join(", ")}">
                        ${this.escapeHtml(n)}${s}
                    </div>
                </td>
                <td>
                    <span class="badge bg-dark">${this.escapeHtml(e.matched_rule||"N/A")}</span>
                </td>
                <td>
                    <span class="badge bg-${i}">${e.priority}</span>
                </td>
                <td>
                    <small class="text-muted">${this.highlightKeywords(o,e.matched_domains)}</small>
                </td>
                <td>
                    <button class="btn btn-sm btn-outline-secondary" onclick="dashboard.viewCertDetails('${e.tbs_sha256}')">
                        <i class="bi bi-eye"></i>
                    </button>
                </td>
            </tr>
        `}highlightKeywords(e,t){if(!t||t.length===0)return this.escapeHtml(e);let n=this.escapeHtml(e);return t.forEach(e=>{const t=this.escapeHtml(e).replace(/[.*+?^${}()|[\]\\]/g,"\\$&"),s=new RegExp(`(${t})`,"gi");n=n.replace(s,'<mark class="bg-warning bg-opacity-50">$1</mark>')}),n}escapeHtml(e){const t=document.createElement("div");return t.textContent=e,t.innerHTML}async viewCertDetails(e){const s=new bootstrap.Modal(document.getElementById("certModal")),n=document.getElementById("certModalBody");n.innerHTML='<div class="text-center"><div class="spinner-border text-primary"></div></div>',s.show();const t=this.matches.find(t=>t.tbs_sha256===e);t&&(n.innerHTML=`
                <div class="row g-3">
                    <div class="col-12">
                        <h6>DNS Names</h6>
                        <div class="alert alert-secondary mb-0">
                            ${t.dns_names.map(e=>`<div>${this.escapeHtml(e)}</div>`).join("")}
                        </div>
                    </div>
                    <div class="col-md-6">
                        <h6>Matched Rule</h6>
                        <p>${this.escapeHtml(t.matched_rule||"N/A")}</p>
                    </div>
                    <div class="col-md-6">
                        <h6>Priority</h6>
                        <p><span class="badge bg-secondary">${t.priority}</span></p>
                    </div>
                    <div class="col-12">
                        <h6>Keywords</h6>
                        <p>${(t.matched_domains||[]).map(e=>`<span class="badge bg-info me-1">${this.escapeHtml(e)}</span>`).join("")}</p>
                    </div>
                    <div class="col-md-6">
                        <h6>TBS SHA256</h6>
                        <p><code class="small">${e}</code></p>
                    </div>
                    <div class="col-md-6">
                        <h6>Cert SHA256</h6>
                        <p><code class="small">${t.cert_sha256}</code></p>
                    </div>
                </div>
            `)}async clearMatches(){if(!confirm("Are you sure you want to clear all matches from memory?"))return;try{const e=await fetch("/matches/clear",{method:"POST"});if(!e.ok)throw new Error("Failed to clear matches");this.showToast("Matches cleared successfully","success"),this.loadMatches()}catch(e){console.error("Error clearing matches:",e),this.showToast("Failed to clear matches","danger")}}prevPage(){this.currentPage>0&&(this.currentPage--,this.renderMatches())}nextPage(){const e=Math.ceil(this.filteredMatches.length/this.pageSize)-1;this.currentPage<e&&(this.currentPage++,this.renderMatches())}startAutoRefresh(){this.refreshInterval=setInterval(()=>{this.loadMatches(),this.loadPerformanceMetrics()},2e4)}showToast(e,t="info"){const n=document.createElement("div");n.className=`alert alert-${t} alert-dismissible fade show position-fixed top-0 end-0 m-3`,n.style.zIndex="9999",n.innerHTML=`
            ${e}
            <button type="button" class="btn-close" data-bs-dismiss="alert"></button>
        `,document.body.appendChild(n),setTimeout(()=>{n.remove()},5e3)}async logout(){try{const e=await fetch("/auth/logout",{method:"POST"});e.ok?window.location.href="/login":this.showToast("Failed to logout","danger")}catch(e){console.error("Error logging out:",e),this.showToast("Failed to logout","danger")}}}const dashboard=new Dashboard