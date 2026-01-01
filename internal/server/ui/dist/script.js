// SmartHPA Manager - Client-side JavaScript

const API_BASE = '/api/v1';
let allItems = [];
let currentItem = null;
let editingItem = null;
let formTriggers = [];

// Permission constants
const PERM_READ = 4;
const PERM_WRITE = 2;
const PERM_DELETE = 1;

// ============================================================================
// Authentication
// ============================================================================

function getAuthToken() {
    return localStorage.getItem('authToken');
}

function getPermission() {
    return parseInt(localStorage.getItem('permission') || '0');
}

function canRead() {
    return (getPermission() & PERM_READ) !== 0;
}

function canWrite() {
    return (getPermission() & PERM_WRITE) !== 0;
}

function canDelete() {
    return (getPermission() & PERM_DELETE) !== 0;
}

function getAuthHeaders() {
    const token = getAuthToken();
    return token ? { 'X-Auth-Token': token } : {};
}

async function checkAuth() {
    const token = getAuthToken();
    if (!token) {
        window.location.href = '/login.html';
        return false;
    }
    
    try {
        const res = await fetch(`${API_BASE}/auth/session`, {
            headers: getAuthHeaders()
        });
        const data = await res.json();
        
        if (!data.authenticated) {
            localStorage.removeItem('authToken');
            window.location.href = '/login.html';
            return false;
        }
        
        // Update user info display
        updateUserInfo(data);
        return true;
    } catch (err) {
        console.error('Auth check failed:', err);
        window.location.href = '/login.html';
        return false;
    }
}

function updateUserInfo(data) {
    const userInfo = document.getElementById('userInfo');
    if (userInfo) {
        const permLabel = getPermissionLabel(data.permission);
        userInfo.innerHTML = `
            <span class="user-name">${data.displayName || data.username}</span>
            <span class="user-permission">${permLabel}</span>
        `;
    }
    
    // Update UI based on permissions
    updateUIPermissions();
}

function getPermissionLabel(perm) {
    if (perm === 7) return 'Admin';
    if (perm === 6) return 'Editor';
    if (perm === 4) return 'Viewer';
    return `Level ${perm}`;
}

function updateUIPermissions() {
    // Hide/show create button based on write permission
    const createBtn = document.querySelector('[onclick="showCreateModal()"]');
    if (createBtn) {
        createBtn.style.display = canWrite() ? 'inline-flex' : 'none';
    }
}

async function logout() {
    const token = getAuthToken();
    if (token) {
        try {
            await fetch(`${API_BASE}/auth/logout`, {
                method: 'POST',
                headers: getAuthHeaders()
            });
        } catch (err) {
            console.error('Logout error:', err);
        }
    }
    localStorage.removeItem('authToken');
    localStorage.removeItem('userName');
    localStorage.removeItem('displayName');
    localStorage.removeItem('permission');
    window.location.href = '/login.html';
}

// Authenticated fetch wrapper
async function authFetch(url, options = {}) {
    const headers = {
        ...options.headers,
        ...getAuthHeaders()
    };
    
    const res = await fetch(url, { ...options, headers });
    
    if (res.status === 401) {
        localStorage.removeItem('authToken');
        window.location.href = '/login.html';
        throw new Error('Session expired');
    }
    
    return res;
}

// Common timezones
const TIMEZONES = [
    'UTC',
    'America/New_York',
    'America/Chicago',
    'America/Denver',
    'America/Los_Angeles',
    'America/Sao_Paulo',
    'Europe/London',
    'Europe/Paris',
    'Europe/Berlin',
    'Asia/Tokyo',
    'Asia/Shanghai',
    'Asia/Singapore',
    'Asia/Kolkata',
    'Asia/Dubai',
    'Australia/Sydney',
    'Pacific/Auckland'
];

// Day abbreviations
const DAYS = [
    { value: 'M', label: 'Mon' },
    { value: 'TU', label: 'Tue' },
    { value: 'W', label: 'Wed' },
    { value: 'TH', label: 'Thu' },
    { value: 'F', label: 'Fri' },
    { value: 'SAT', label: 'Sat' },
    { value: 'SUN', label: 'Sun' }
];

// ============================================================================
// Data Fetching
// ============================================================================

async function fetchSmartHPAs() {
    const namespace = document.getElementById('namespaceFilter').value;
    const url = namespace ? `${API_BASE}/smarthpa?namespace=${namespace}` : `${API_BASE}/smarthpa`;
    
    try {
        const res = await authFetch(url);
        const data = await res.json();
        allItems = data.items || [];
        renderList(allItems);
        updateStats(allItems);
    } catch (err) {
        document.getElementById('smartHPAList').innerHTML = `
            <div class="empty-state">
                <div class="empty-icon">❌</div>
                <div class="empty-title">Error</div>
                <div class="empty-text">${err.message}</div>
            </div>`;
    }
}

async function fetchNamespaces() {
    try {
        const res = await authFetch(`${API_BASE}/namespaces`);
        const data = await res.json();
        const select = document.getElementById('namespaceFilter');
        (data.namespaces || []).sort().forEach(ns => {
            const opt = document.createElement('option');
            opt.value = ns;
            opt.textContent = ns;
            select.appendChild(opt);
        });
    } catch (err) {
        console.error('Failed to fetch namespaces:', err);
    }
}

// ============================================================================
// Stats & Rendering
// ============================================================================

function updateStats(items) {
    const total = items.length;
    const ready = items.filter(i => i.conditions?.find(c => c.type === 'Ready' && c.status === 'True')).length;
    const triggers = items.reduce((sum, i) => sum + (i.triggers?.length || 0), 0);
    
    document.getElementById('totalCount').textContent = total;
    document.getElementById('readyCount').textContent = ready;
    document.getElementById('triggerCount').textContent = triggers;
}

function renderList(items) {
    if (items.length === 0) {
        document.getElementById('smartHPAList').innerHTML = `
            <div class="empty-state">
                <div class="empty-icon">📋</div>
                <div class="empty-title">No SmartHPAs Found</div>
                <div class="empty-text">Create your first SmartHPA to get started!</div>
            </div>`;
        return;
    }

    const rows = items.map(item => {
        const status = item.conditions?.find(c => c.type === 'Ready');
        const statusBadge = status?.status === 'True' 
            ? '<span class="badge badge-success">● Ready</span>'
            : '<span class="badge badge-warning">○ Pending</span>';
        const triggerCount = item.triggers?.length || 0;
        
        return `
            <tr class="clickable" onclick="openDetail('${item.namespace}', '${item.name}')">
                <td><span class="item-name">${item.name}</span></td>
                <td><span class="item-namespace">${item.namespace}</span></td>
                <td>${item.hpaObjectRef?.name || '<span style="color:var(--text-muted)">—</span>'}</td>
                <td><span class="trigger-count">${triggerCount} trigger${triggerCount !== 1 ? 's' : ''}</span></td>
                <td>${statusBadge}</td>
                <td class="actions-cell" onclick="event.stopPropagation()">
                    <button class="btn btn-icon btn-secondary" onclick="openDetail('${item.namespace}', '${item.name}')">👁</button>
                    ${canDelete() ? `<button class="btn btn-icon btn-danger" onclick="deleteSmartHPA('${item.namespace}', '${item.name}')">🗑</button>` : ''}
                </td>
            </tr>`;
    }).join('');

    document.getElementById('smartHPAList').innerHTML = `
        <div class="table-container">
            <table>
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Namespace</th>
                        <th>HPA Reference</th>
                        <th>Triggers</th>
                        <th>Status</th>
                        <th style="text-align:right">Actions</th>
                    </tr>
                </thead>
                <tbody>${rows}</tbody>
            </table>
        </div>`;
}

// ============================================================================
// Detail Panel
// ============================================================================

function openDetail(namespace, name) {
    const item = allItems.find(i => i.namespace === namespace && i.name === name);
    if (!item) return;
    
    currentItem = item;
    document.getElementById('detailName').textContent = item.name;
    document.getElementById('detailNamespace').textContent = `Namespace: ${item.namespace}`;
    
    let html = '';
    
    // HPA Reference Section
    html += '<div class="detail-section"><div class="detail-section-title">HPA Reference</div>';
    if (item.hpaObjectRef) {
        html += `
            <div class="hpa-card">
                <div class="hpa-card-row">
                    <span class="hpa-label">Name</span>
                    <span class="hpa-value">${item.hpaObjectRef.name}</span>
                </div>
                <div class="hpa-card-row">
                    <span class="hpa-label">Namespace</span>
                    <span class="hpa-value">${item.hpaObjectRef.namespace || item.namespace}</span>
                </div>
            </div>`;
    } else {
        html += '<div style="color:var(--text-muted)">No HPA reference configured</div>';
    }
    html += '</div>';
    
    // Triggers Section
    html += `<div class="detail-section"><div class="detail-section-title">Triggers (${item.triggers?.length || 0})</div>`;
    if (item.triggers && item.triggers.length > 0) {
        html += item.triggers.map(t => `
            <div class="trigger-card">
                <div class="trigger-header">
                    <span class="trigger-name">${t.name}</span>
                    <span class="trigger-priority">Priority: ${t.priority || 0}</span>
                </div>
                <div class="trigger-body">
                    <div class="trigger-grid">
                        <div class="trigger-item">
                            <div class="trigger-item-label">Schedule</div>
                            <div class="trigger-item-value">${t.interval?.recurring || 'N/A'}</div>
                        </div>
                        <div class="trigger-item">
                            <div class="trigger-item-label">Timezone</div>
                            <div class="trigger-item-value">${t.timezone || 'UTC'}</div>
                        </div>
                        <div class="trigger-item">
                            <div class="trigger-item-label">Start Time</div>
                            <div class="trigger-item-value">${t.startTime || 'N/A'}</div>
                        </div>
                        <div class="trigger-item">
                            <div class="trigger-item-label">End Time</div>
                            <div class="trigger-item-value">${t.endTime || 'N/A'}</div>
                        </div>
                    </div>
                    ${t.startHPAConfig ? `
                        <div class="config-box">
                            <div class="config-title">Start Config</div>
                            <div class="config-values">
                                <span class="config-value"><span>Min:</span> ${t.startHPAConfig.minReplicas || '-'}</span>
                                <span class="config-value"><span>Max:</span> ${t.startHPAConfig.maxReplicas || '-'}</span>
                            </div>
                        </div>` : ''}
                    ${t.endHPAConfig ? `
                        <div class="config-box">
                            <div class="config-title">End Config</div>
                            <div class="config-values">
                                <span class="config-value"><span>Min:</span> ${t.endHPAConfig.minReplicas || '-'}</span>
                                <span class="config-value"><span>Max:</span> ${t.endHPAConfig.maxReplicas || '-'}</span>
                            </div>
                        </div>` : ''}
                </div>
            </div>`).join('');
    } else {
        html += '<div style="color:var(--text-muted)">No triggers configured</div>';
    }
    html += '</div>';
    
    // Conditions Section
    html += '<div class="detail-section"><div class="detail-section-title">Status</div><div class="conditions-list">';
    if (item.conditions && item.conditions.length > 0) {
        html += item.conditions.map(c => {
            const isSuccess = c.status === 'True';
            return `
                <div class="condition-item">
                    <div class="condition-icon ${isSuccess ? 'success' : 'warning'}">${isSuccess ? '✓' : '!'}</div>
                    <div class="condition-text">
                        <div class="condition-type">${c.type}</div>
                        <div class="condition-message">${c.message || c.reason || ''}</div>
                    </div>
                </div>`;
        }).join('');
    } else {
        html += '<div style="color:var(--text-muted)">No conditions</div>';
    }
    html += '</div></div>';
    
    document.getElementById('detailBody').innerHTML = html;
    document.getElementById('overlay').classList.add('active');
    document.getElementById('detailPanel').classList.add('active');
    
    // Show/hide action buttons based on permissions
    const editBtn = document.getElementById('editBtn');
    const deleteBtn = document.getElementById('deleteBtn');
    if (editBtn) editBtn.style.display = canWrite() ? 'inline-flex' : 'none';
    if (deleteBtn) deleteBtn.style.display = canDelete() ? 'inline-flex' : 'none';
}

function closeDetail() {
    document.getElementById('overlay').classList.remove('active');
    document.getElementById('detailPanel').classList.remove('active');
    currentItem = null;
}

function editCurrentItem() {
    if (currentItem) {
        const item = currentItem;  // Save reference before closeDetail() nullifies it
        closeDetail();
        editSmartHPA(item);
    }
}

function deleteCurrentItem() {
    if (currentItem) {
        deleteSmartHPA(currentItem.namespace, currentItem.name);
        closeDetail();
    }
}

// ============================================================================
// Modal (Create/Edit) - Interactive Trigger Editor
// ============================================================================

function showCreateModal() {
    editingItem = null;
    document.getElementById('modalTitle').textContent = 'Create SmartHPA';
    document.getElementById('formName').value = '';
    document.getElementById('formName').disabled = false;
    document.getElementById('formNamespace').value = 'default';
    document.getElementById('formNamespace').disabled = false;
    document.getElementById('formHPAName').value = '';
    
    // Initialize with one default trigger
    formTriggers = [{
        name: 'business-hours',
        priority: 100,
        timezone: 'America/Los_Angeles',
        startTime: '09:00:00',
        endTime: '17:00:00',
        interval: { recurring: 'M,TU,W,TH,F' },
        startHPAConfig: { minReplicas: 3, maxReplicas: 10 },
        endHPAConfig: { minReplicas: 1, maxReplicas: 5 }
    }];
    
    renderTriggersForm();
    document.getElementById('overlay').classList.add('active');
    document.getElementById('modal').classList.add('active');
}

function editSmartHPA(item) {
    editingItem = item;
    document.getElementById('modalTitle').textContent = 'Edit SmartHPA';
    document.getElementById('formName').value = item.name;
    document.getElementById('formName').disabled = true;
    document.getElementById('formNamespace').value = item.namespace;
    document.getElementById('formNamespace').disabled = true;
    document.getElementById('formHPAName').value = item.hpaObjectRef?.name || '';
    
    // Copy triggers for editing
    formTriggers = JSON.parse(JSON.stringify(item.triggers || []));
    
    renderTriggersForm();
    document.getElementById('overlay').classList.add('active');
    document.getElementById('modal').classList.add('active');
}

function closeModal() {
    document.getElementById('overlay').classList.remove('active');
    document.getElementById('modal').classList.remove('active');
    editingItem = null;
    formTriggers = [];
}

// ============================================================================
// Trigger Form Rendering
// ============================================================================

function renderTriggersForm() {
    const container = document.getElementById('triggersContainer');
    const noTriggersMsg = document.getElementById('noTriggersMessage');
    
    if (formTriggers.length === 0) {
        container.innerHTML = '';
        noTriggersMsg.style.display = 'block';
        return;
    }
    
    noTriggersMsg.style.display = 'none';
    
    container.innerHTML = formTriggers.map((trigger, index) => {
        const selectedDays = (trigger.interval?.recurring || '').split(',').filter(d => d);
        
        return `
            <div class="trigger-form-card" data-index="${index}">
                <div class="trigger-form-header">
                    <div class="trigger-form-title">
                        <span class="trigger-index">${index + 1}</span>
                        <input type="text" class="form-input" value="${trigger.name || ''}" 
                               onchange="updateTriggerField(${index}, 'name', this.value)"
                               placeholder="Trigger name" style="background:transparent;border:none;color:var(--cyan);font-weight:600;padding:0;width:150px;">
                    </div>
                    <div class="trigger-form-actions">
                        <button type="button" class="btn-collapse" onclick="toggleTrigger(${index})">▼</button>
                        <button type="button" class="btn-delete-trigger" onclick="removeTrigger(${index})">🗑</button>
                    </div>
                </div>
                <div class="trigger-form-body" id="triggerBody${index}">
                    <div class="form-row">
                        <div class="form-group">
                            <label class="form-label">Priority</label>
                            <input type="number" class="form-input" value="${trigger.priority || 0}" 
                                   onchange="updateTriggerField(${index}, 'priority', parseInt(this.value))"
                                   placeholder="100">
                        </div>
                        <div class="form-group">
                            <label class="form-label">Timezone</label>
                            <select class="form-select" onchange="updateTriggerField(${index}, 'timezone', this.value)">
                                ${TIMEZONES.map(tz => `<option value="${tz}" ${trigger.timezone === tz ? 'selected' : ''}>${tz}</option>`).join('')}
                            </select>
                        </div>
                    </div>
                    
                    <div class="form-row">
                        <div class="form-group">
                            <label class="form-label">Start Time</label>
                            <input type="time" class="form-input" value="${(trigger.startTime || '09:00:00').substring(0,5)}" 
                                   onchange="updateTriggerField(${index}, 'startTime', this.value + ':00')"
                                   step="60">
                        </div>
                        <div class="form-group">
                            <label class="form-label">End Time</label>
                            <input type="time" class="form-input" value="${(trigger.endTime || '17:00:00').substring(0,5)}" 
                                   onchange="updateTriggerField(${index}, 'endTime', this.value + ':00')"
                                   step="60">
                        </div>
                    </div>
                    
                    <div class="form-group">
                        <label class="form-label">Schedule (Days)</label>
                        <div class="day-selector">
                            ${DAYS.map(day => `
                                <input type="checkbox" class="day-checkbox" id="day${index}_${day.value}" 
                                       ${selectedDays.includes(day.value) ? 'checked' : ''}
                                       onchange="updateTriggerDays(${index})">
                                <label class="day-label" for="day${index}_${day.value}">${day.label}</label>
                            `).join('')}
                        </div>
                    </div>
                    
                    <div class="config-subsection">
                        <div class="config-subsection-title">Start HPA Config (when trigger activates)</div>
                        <div class="form-row">
                            <div class="form-group">
                                <label class="form-label">Min Replicas</label>
                                <input type="number" class="form-input" value="${trigger.startHPAConfig?.minReplicas || 1}" 
                                       onchange="updateTriggerConfig(${index}, 'startHPAConfig', 'minReplicas', parseInt(this.value))"
                                       min="0">
                            </div>
                            <div class="form-group">
                                <label class="form-label">Max Replicas</label>
                                <input type="number" class="form-input" value="${trigger.startHPAConfig?.maxReplicas || 10}" 
                                       onchange="updateTriggerConfig(${index}, 'startHPAConfig', 'maxReplicas', parseInt(this.value))"
                                       min="1">
                            </div>
                        </div>
                    </div>
                    
                    <div class="config-subsection">
                        <div class="config-subsection-title">End HPA Config (when trigger deactivates)</div>
                        <div class="form-row">
                            <div class="form-group">
                                <label class="form-label">Min Replicas</label>
                                <input type="number" class="form-input" value="${trigger.endHPAConfig?.minReplicas || 1}" 
                                       onchange="updateTriggerConfig(${index}, 'endHPAConfig', 'minReplicas', parseInt(this.value))"
                                       min="0">
                            </div>
                            <div class="form-group">
                                <label class="form-label">Max Replicas</label>
                                <input type="number" class="form-input" value="${trigger.endHPAConfig?.maxReplicas || 5}" 
                                       onchange="updateTriggerConfig(${index}, 'endHPAConfig', 'maxReplicas', parseInt(this.value))"
                                       min="1">
                            </div>
                        </div>
                    </div>
                </div>
            </div>`;
    }).join('');
}

function addTrigger() {
    formTriggers.push({
        name: `trigger-${formTriggers.length + 1}`,
        priority: 50,
        timezone: 'UTC',
        startTime: '09:00:00',
        endTime: '17:00:00',
        interval: { recurring: 'M,TU,W,TH,F' },
        startHPAConfig: { minReplicas: 2, maxReplicas: 10 },
        endHPAConfig: { minReplicas: 1, maxReplicas: 5 }
    });
    renderTriggersForm();
}

function removeTrigger(index) {
    if (formTriggers.length === 1) {
        if (!confirm('Remove the last trigger? The SmartHPA will have no triggers.')) return;
    }
    formTriggers.splice(index, 1);
    renderTriggersForm();
}

function toggleTrigger(index) {
    const body = document.getElementById(`triggerBody${index}`);
    const btn = body.previousElementSibling.querySelector('.btn-collapse');
    body.classList.toggle('collapsed');
    btn.classList.toggle('collapsed');
}

function updateTriggerField(index, field, value) {
    formTriggers[index][field] = value;
}

function updateTriggerDays(index) {
    const selectedDays = DAYS
        .filter(day => document.getElementById(`day${index}_${day.value}`).checked)
        .map(day => day.value);
    
    if (!formTriggers[index].interval) {
        formTriggers[index].interval = {};
    }
    formTriggers[index].interval.recurring = selectedDays.join(',');
}

function updateTriggerConfig(index, configType, field, value) {
    if (!formTriggers[index][configType]) {
        formTriggers[index][configType] = {};
    }
    formTriggers[index][configType][field] = value;
}

// ============================================================================
// CRUD Operations
// ============================================================================

async function deleteSmartHPA(namespace, name) {
    if (!canDelete()) {
        showNotification('Permission denied: delete access required', 'error');
        return;
    }
    
    if (!confirm(`Delete SmartHPA "${name}"?`)) return;
    
    try {
        const res = await authFetch(`${API_BASE}/smarthpa/${namespace}/${name}`, { method: 'DELETE' });
        if (res.ok) {
            showNotification('SmartHPA deleted', 'success');
            fetchSmartHPAs();
        } else {
            throw new Error(await res.text());
        }
    } catch (err) {
        showNotification(`Error: ${err.message}`, 'error');
    }
}

// Form submission handler
document.getElementById('smartHPAForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const name = document.getElementById('formName').value;
    const namespace = document.getElementById('formNamespace').value;
    const hpaName = document.getElementById('formHPAName').value;

    const payload = {
        name: name,
        namespace: namespace,
        hpaObjectRef: hpaName ? { name: hpaName, namespace: namespace } : null,
        triggers: formTriggers
    };

    if (!canWrite()) {
        showNotification('Permission denied: write access required', 'error');
        return;
    }

    try {
        let res;
        if (editingItem) {
            res = await authFetch(`${API_BASE}/smarthpa/${namespace}/${name}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
        } else {
            res = await authFetch(`${API_BASE}/smarthpa`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
        }

        if (res.ok) {
            showNotification(editingItem ? 'SmartHPA updated' : 'SmartHPA created', 'success');
            closeModal();
            fetchSmartHPAs();
        } else {
            throw new Error(await res.text());
        }
    } catch (err) {
        showNotification(`Error: ${err.message}`, 'error');
    }
});

// ============================================================================
// Notifications
// ============================================================================

function showNotification(msg, type) {
    const el = document.getElementById('notification');
    el.textContent = msg;
    el.className = `notification active ${type}`;
    setTimeout(() => {
        el.className = 'notification';
    }, 3000);
}

// ============================================================================
// Event Listeners & Initialization
// ============================================================================

document.getElementById('namespaceFilter').addEventListener('change', fetchSmartHPAs);

// Handle overlay click to close modal or detail
document.getElementById('overlay').addEventListener('click', (e) => {
    if (e.target === document.getElementById('overlay')) {
        if (document.getElementById('modal').classList.contains('active')) {
            closeModal();
        } else if (document.getElementById('detailPanel').classList.contains('active')) {
            closeDetail();
        }
    }
});

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        if (document.getElementById('modal').classList.contains('active')) {
            closeModal();
        } else if (document.getElementById('detailPanel').classList.contains('active')) {
            closeDetail();
        }
    }
});

// Initial load with auth check
async function init() {
    const isAuth = await checkAuth();
    if (isAuth) {
        fetchNamespaces();
        fetchSmartHPAs();
    }
}

init();
