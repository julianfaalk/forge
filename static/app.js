$(document).ready(function() {
    // State
    let tasks = [];
    let projects = [];
    let taskTypes = [];
    let config = {};
    let ws = null;
    let currentTaskId = null;
    let currentProjectId = null;  // For project modal editing
    let currentTaskTypeId = null; // For task type modal editing
    let selectedProjectFilter = ''; // For filtering tasks by project
    let autoScroll = true;
    let isProgrammaticScroll = false; // Flag to ignore programmatic scrolls
    let scrollTimeout = null; // Debounce timer for scroll detection
    let folderBrowserPath = '';
    let selectedFolderPath = '';
    let folderBrowserTarget = 'task'; // 'task', 'project', or 'scan'
    let branchRules = []; // Branch rules for current project being edited
    let scannedRepos = []; // Scan results
    let collapsedFolders = {}; // Track collapsed state of folders
    let githubUser = null; // GitHub user info (username, avatar_url, etc.)
    let sidebarOpen = false; // Track sidebar state
    let pendingAttachments = []; // Files queued for upload before task is saved
    let activeMobileTab = 'backlog'; // Active tab for mobile view
    let currentAttachments = []; // Attachments for current task
    let lightboxIndex = 0; // Current lightbox image index

    // Initialize
    init();

    // Load collapsed state from localStorage
    function loadCollapsedState() {
        try {
            const saved = localStorage.getItem('grinder_collapsed_folders');
            if (saved) {
                collapsedFolders = JSON.parse(saved);
            }
        } catch (e) {
            collapsedFolders = {};
        }
    }

    // Save collapsed state to localStorage
    function saveCollapsedState() {
        try {
            localStorage.setItem('grinder_collapsed_folders', JSON.stringify(collapsedFolders));
        } catch (e) {
            // Ignore storage errors
        }
    }

    // ============================================================================
    // THEME MANAGEMENT
    // ============================================================================

    // System preference media query
    const systemThemeQuery = window.matchMedia('(prefers-color-scheme: dark)');

    /**
     * Gets the saved theme preference from localStorage
     * @returns {string} 'dark' | 'light' | 'system'
     */
    function getSavedTheme() {
        try {
            return localStorage.getItem('grinder-theme') || 'dark';
        } catch (e) {
            return 'dark';
        }
    }

    /**
     * Saves theme preference to localStorage
     * @param {string} theme - 'dark' | 'light' | 'system'
     */
    function saveTheme(theme) {
        try {
            localStorage.setItem('grinder-theme', theme);
        } catch (e) {
            // Ignore storage errors
        }
    }

    /**
     * Resolves the effective theme based on preference
     * @param {string} preference - 'dark' | 'light' | 'system'
     * @returns {string} 'dark' | 'light'
     */
    function resolveTheme(preference) {
        if (preference === 'system') {
            return systemThemeQuery.matches ? 'dark' : 'light';
        }
        return preference;
    }

    /**
     * Applies theme to the document
     * @param {string} theme - 'dark' | 'light'
     */
    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
    }

    /**
     * Initializes the theme on page load
     * Called immediately to prevent flash
     */
    function initTheme() {
        const preference = getSavedTheme();
        const theme = resolveTheme(preference);
        applyTheme(theme);
    }

    /**
     * Sets a new theme preference (called when user changes settings)
     * @param {string} preference - 'dark' | 'light' | 'system'
     */
    function setThemePreference(preference) {
        saveTheme(preference);
        const theme = resolveTheme(preference);
        applyTheme(theme);
    }

    /**
     * Listen for system theme changes when using 'system' preference
     */
    function setupSystemThemeListener() {
        systemThemeQuery.addEventListener('change', function(e) {
            const preference = getSavedTheme();
            if (preference === 'system') {
                applyTheme(e.matches ? 'dark' : 'light');
            }
        });
    }

    // Initialize theme immediately (before DOMContentLoaded)
    initTheme();
    setupSystemThemeListener();

    // ============================================================================
    // SIDEBAR STATE MANAGEMENT
    // ============================================================================

    /**
     * Load saved selected project from localStorage
     * @returns {string} Project ID or empty string for all projects
     */
    function loadSelectedProject() {
        try {
            return localStorage.getItem('grinder-selected-project') || '';
        } catch (e) {
            return '';
        }
    }

    /**
     * Save selected project to localStorage
     * @param {string} projectId - Project ID or empty string for all projects
     */
    function saveSelectedProject(projectId) {
        try {
            localStorage.setItem('grinder-selected-project', projectId);
        } catch (e) {
            // Ignore storage errors
        }
    }

    /**
     * Load sidebar state from localStorage
     * @returns {boolean} true if sidebar should be open, false otherwise
     */
    function loadSidebarState() {
        try {
            return localStorage.getItem('grinder-sidebar-open') === 'true';
        } catch (e) {
            return false;
        }
    }

    /**
     * Save sidebar state to localStorage
     * @param {boolean} isOpen - Whether sidebar is open
     */
    function saveSidebarState(isOpen) {
        try {
            localStorage.setItem('grinder-sidebar-open', isOpen ? 'true' : 'false');
        } catch (e) {
            // Ignore storage errors
        }
    }

    /**
     * Open the sidebar
     */
    function openSidebar() {
        sidebarOpen = true;
        $('#sidebar').addClass('open');
        $('#sidebarOverlay').addClass('active');
        $('#sidebarResizeHandle').addClass('visible');
        // Note: sidebar state is NOT persisted - always starts closed
    }

    /**
     * Close the sidebar
     */
    function closeSidebar() {
        sidebarOpen = false;
        $('#sidebar').removeClass('open');
        $('#sidebarOverlay').removeClass('active');
        $('#sidebarResizeHandle').removeClass('visible');
        // Note: sidebar state is NOT persisted - always starts closed
    }

    /**
     * Toggle sidebar open/closed
     */
    function toggleSidebar() {
        if (sidebarOpen) {
            closeSidebar();
        } else {
            openSidebar();
        }
    }

    /**
     * Update the selected project display in header
     * @param {string} projectId - Project ID or empty string for all projects
     */
    function updateSelectedProjectDisplay(projectId) {
        let displayName = 'All Projects';
        let displayTitle = 'All Projects';

        if (projectId) {
            const project = projects.find(p => p.id === projectId);
            if (project) {
                displayName = project.name;
                displayTitle = project.path || project.name;
            } else {
                // Project no longer exists, fallback to all projects
                selectedProjectFilter = '';
                saveSelectedProject('');
            }
        }

        $('#selectedProjectName').text(displayName).attr('title', displayTitle);
        updateBranchSelector(projectId);
    }

    /**
     * Update branch selector in header
     */
    function updateBranchSelector(projectId) {
        const $selector = $('#branchSelector');
        const $branchName = $('#currentBranchName');
        const $pullBtn = $('#branchPullBtn');
        const $pushBtn = $('#pushBtn');

        if (!projectId) {
            $selector.addClass('hidden');
            $pushBtn.addClass('hidden');
            return;
        }

        $.get('/api/projects/' + projectId + '/git-info')
            .done(function(data) {
                if (data.current_branch) {
                    $branchName.text(data.current_branch).attr('title', data.current_branch);
                    $selector.removeClass('hidden');
                    checkBranchBehind(projectId, data.current_branch);
                    // Update push status for trunk-based development
                    updatePushStatus(projectId);
                } else {
                    $selector.addClass('hidden');
                    $pushBtn.addClass('hidden');
                }
            })
            .fail(function() {
                $selector.addClass('hidden');
                $pushBtn.addClass('hidden');
            });
    }

    /**
     * Update push status badge (Trunk-based development)
     */
    function updatePushStatus(projectId) {
        const $pushBtn = $('#pushBtn');
        const $pushBadge = $('#pushBadge');

        if (!projectId) {
            $pushBtn.addClass('hidden');
            return;
        }

        $.get('/api/projects/' + projectId + '/push-status')
            .done(function(data) {
                if (data.has_remote && data.unpushed_count > 0) {
                    $pushBadge.text(data.unpushed_count).removeClass('hidden');
                    $pushBtn.removeClass('hidden');
                } else if (data.has_remote) {
                    $pushBadge.addClass('hidden');
                    $pushBtn.removeClass('hidden');
                } else {
                    $pushBtn.addClass('hidden');
                }
            })
            .fail(function() {
                $pushBtn.addClass('hidden');
            });
    }

    /**
     * Push to remote (Trunk-based development)
     */
    function pushToRemote(projectId) {
        showToast('Committing & pushing...', 'info');

        $.post('/api/projects/' + projectId + '/push')
            .done(function(data) {
                showToast(data.message || 'Push successful!', 'success');
                updatePushStatus(projectId);
            })
            .fail(function(err) {
                const msg = err.responseJSON?.error || 'Push failed';
                showToast(msg, 'error');
            });
    }

    /**
     * Rollback task to its rollback tag (Trunk-based development)
     */
    function rollbackTask(taskId) {
        if (!confirm('Are you sure you want to rollback this task? All changes made by this task will be undone.')) {
            return;
        }

        showToast('Rolling back...', 'info');

        $.post('/api/tasks/' + taskId + '/rollback')
            .done(function(data) {
                showToast('Task rolled back successfully!', 'success');
            })
            .fail(function(err) {
                const msg = err.responseJSON?.error || 'Rollback failed';
                showToast(msg, 'error');
            });
    }

    /**
     * Set working branch for project (Trunk-based development)
     */
    function setWorkingBranch(projectId, branch, create) {
        showToast(create ? 'Creating branch, committing & pushing...' : 'Setting working branch...', 'info');

        $.ajax({
            url: '/api/projects/' + projectId + '/working-branch',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ branch: branch, create: !!create })
        })
        .done(function(data) {
            const msg = create
                ? 'Branch "' + branch + '" created and pushed'
                : 'Working branch set to: ' + branch;
            showToast(msg, 'success');
            closeBranchDropdown();
            updateBranchSelector(projectId);
            updatePushStatus(projectId); // Update push badge
            loadProjects(); // Refresh project list
        })
        .fail(function(err) {
            const msg = err.responseJSON?.error || 'Failed to set working branch';
            showToast(msg, 'error');
        });
    }

    /**
     * Check if branch is behind remote and show pull button
     */
    function checkBranchBehind(projectId, branch) {
        const $pullBtn = $('#branchPullBtn');

        $.get('/api/projects/' + projectId + '/branch-status?branch=' + encodeURIComponent(branch))
            .done(function(data) {
                if (data.behind > 0) {
                    $pullBtn.removeClass('hidden').attr('title', 'Pull ' + data.behind + ' commit(s)');
                } else {
                    $pullBtn.addClass('hidden');
                }
            })
            .fail(function() {
                $pullBtn.addClass('hidden');
            });
    }

    /**
     * Load branches into dropdown
     */
    function loadBranchDropdown(projectId) {
        const $list = $('#branchDropdownList');
        $list.html('<div class="branch-dropdown-item">Loading...</div>');

        $.when(
            $.get('/api/projects/' + projectId + '/branches'),
            $.get('/api/projects/' + projectId + '/git-info')
        ).done(function(branchRes, gitRes) {
            const branches = branchRes[0].branches || [];
            const currentBranch = gitRes[0].current_branch || '';

            $list.empty();

            // Only local branches, sorted with main/master first
            const localBranches = branches.filter(b => !b.startsWith('origin/'));
            localBranches.sort((a, b) => {
                if (a === 'main' || a === 'master') return -1;
                if (b === 'main' || b === 'master') return 1;
                return a.localeCompare(b);
            });

            localBranches.forEach(function(branch) {
                const isActive = branch === currentBranch;
                $list.append(`
                    <div class="branch-dropdown-item ${isActive ? 'active' : ''}" data-branch="${escapeHtml(branch)}">
                        <svg class="branch-item-icon" viewBox="0 0 16 16" fill="currentColor">
                            <path d="M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25Zm-6 0a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Zm8.25-.75a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM4.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z"/>
                        </svg>
                        <span class="branch-item-name">${escapeHtml(branch)}</span>
                        ${isActive ? '<span class="branch-item-badge">current</span>' : ''}
                    </div>
                `);
            });

            if (localBranches.length === 0) {
                $list.html('<div class="branch-dropdown-item">No branches</div>');
            }

            // Separator and "Create new branch" option
            $list.append('<div class="branch-dropdown-separator"></div>');
            $list.append(`
                <div class="branch-dropdown-item branch-create-new" data-action="create-branch">
                    <svg class="branch-item-icon" viewBox="0 0 16 16" fill="currentColor">
                        <path d="M8 2a.75.75 0 0 1 .75.75v4.5h4.5a.75.75 0 0 1 0 1.5h-4.5v4.5a.75.75 0 0 1-1.5 0v-4.5h-4.5a.75.75 0 0 1 0-1.5h4.5v-4.5A.75.75 0 0 1 8 2Z"/>
                    </svg>
                    <span class="branch-item-name">Create new branch</span>
                </div>
            `);
        }).fail(function() {
            $list.html('<div class="branch-dropdown-item">Error loading</div>');
        });
    }

    /**
     * Switch to a branch
     */
    function switchToBranch(projectId, branch) {
        showToast('Switching to ' + branch + '...', 'info');

        $.post('/api/projects/' + projectId + '/checkout', JSON.stringify({ branch: branch }), null, 'json')
            .done(function(data) {
                if (data.success) {
                    showToast('Switched to ' + branch, 'success');
                    closeBranchDropdown();
                    updateBranchSelector(projectId);
                } else {
                    showToast(data.error || 'Failed to switch', 'error');
                }
            })
            .fail(function(xhr) {
                showToast(xhr.responseJSON?.error || 'Failed to switch', 'error');
            });
    }

    /**
     * Pull latest changes
     */
    function pullBranch(projectId) {
        const $btn = $('#branchPullBtn');
        $btn.prop('disabled', true);
        showToast('Pulling...', 'info');

        $.post('/api/projects/' + projectId + '/pull')
            .done(function(data) {
                if (data.success) {
                    showToast('Pulled successfully', 'success');
                    $btn.addClass('hidden');
                } else {
                    showToast(data.error || 'Failed to pull', 'error');
                }
            })
            .fail(function(xhr) {
                showToast(xhr.responseJSON?.error || 'Failed to pull', 'error');
            })
            .always(function() {
                $btn.prop('disabled', false);
            });
    }

    function toggleBranchDropdown() {
        const $selector = $('#branchSelector');
        const $dropdown = $('#branchDropdown');

        if ($dropdown.hasClass('hidden')) {
            $selector.addClass('open');
            $dropdown.removeClass('hidden');
            if (selectedProjectFilter) loadBranchDropdown(selectedProjectFilter);
        } else {
            closeBranchDropdown();
        }
    }

    function closeBranchDropdown() {
        $('#branchSelector').removeClass('open');
        $('#branchDropdown').addClass('hidden');
    }

    /**
     * Select a project and update UI
     * @param {string} projectId - Project ID or empty string for all projects
     * @param {boolean} closeSidebarAfter - Whether to close sidebar after selection
     */
    function selectProject(projectId, closeSidebarAfter) {
        selectedProjectFilter = projectId;
        saveSelectedProject(projectId);

        // Update sidebar active state
        $('.project-item').removeClass('active');
        $(`.project-item[data-project-id="${projectId}"]`).addClass('active');

        // Update header display
        updateSelectedProjectDisplay(projectId);

        // Filter tasks
        renderAllTasks();

        // Close sidebar if requested
        if (closeSidebarAfter !== false) {
            closeSidebar();
        }
    }

    /**
     * Initialize sidebar state from localStorage
     */
    function initSidebarState() {
        // Load saved project selection
        const savedProject = loadSelectedProject();
        selectedProjectFilter = savedProject;

        // Validate saved project exists (if not empty)
        if (savedProject) {
            const projectExists = projects.some(p => p.id === savedProject);
            if (!projectExists) {
                selectedProjectFilter = '';
                saveSelectedProject('');
            }
        }

        // Update header display
        updateSelectedProjectDisplay(selectedProjectFilter);

        // Sidebar always starts closed (no persistence)
        sidebarOpen = false;
        $('#sidebar').removeClass('open');
        $('#sidebarOverlay').removeClass('active');
    }

    // ============================================================================

    function init() {
        loadCollapsedState();
        loadConfig();
        loadProjects();
        loadTaskTypes();
        loadTasks();
        connectWebSocket();
        setupEventListeners();
        setupDragAndDrop();
        setupSidebarResize();
        setupMobileTabNavigation();

        // Remove no-transitions class after initial load to enable smooth theme transitions
        // Use a small delay to ensure all initial rendering is complete
        setTimeout(function() {
            document.documentElement.classList.remove('no-transitions');
        }, 100);

        // Update theme toggle label to show current theme
        const labels = { 'dark': 'Dark', 'light': 'Light', 'system': 'System' };
        const currentTheme = getSavedTheme();
        $('.theme-toggle-label').text(labels[currentTheme]);
    }

    // API Functions
    function loadConfig() {
        $.get('/api/config')
            .done(function(data) {
                config = data;
                // Check GitHub connection after config is loaded
                checkGithubConnection();
            })
            .fail(function(xhr) {
                showToast('Error loading configuration', 'error');
            });
    }

    function loadProjects() {
        $.get('/api/projects')
            .done(function(data) {
                projects = data || [];
                renderProjectList();
                populateProjectSelect();
                // Initialize sidebar state after projects are loaded
                initSidebarState();
            })
            .fail(function(xhr) {
                showToast('Error loading projects', 'error');
            });
    }

    function loadTaskTypes() {
        $.get('/api/task-types')
            .done(function(data) {
                taskTypes = data || [];
                renderTaskTypeList();
                populateTaskTypeSelect();
            })
            .fail(function(xhr) {
                showToast('Error loading task types', 'error');
            });
    }

    function loadTasks() {
        $.get('/api/tasks')
            .done(function(data) {
                tasks = data || [];
                renderAllTasks();
            })
            .fail(function(xhr) {
                showToast('Error loading tasks', 'error');
            });
    }

    function saveTask(taskData) {
        const isNew = !taskData.id;
        const url = isNew ? '/api/tasks' : '/api/tasks/' + taskData.id;
        const method = isNew ? 'POST' : 'PUT';

        $.ajax({
            url: url,
            method: method,
            contentType: 'application/json',
            data: JSON.stringify(taskData)
        })
        .done(function(task) {
            const idx = tasks.findIndex(t => t.id === task.id);
            if (idx !== -1) {
                tasks[idx] = task;
                renderAllTasks();
            }

            // Upload pending attachments for new tasks
            if (isNew && pendingAttachments.length > 0) {
                uploadPendingAttachments(task.id);
            }

            closeModal();
            showToast(isNew ? 'Task created' : 'Task saved', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error saving';
            showToast(msg, 'error');
        });
    }

    function deleteTask(taskId) {
        $.ajax({
            url: '/api/tasks/' + taskId,
            method: 'DELETE'
        })
        .done(function() {
            tasks = tasks.filter(t => t.id !== taskId);
            renderAllTasks();
            closeModal();
            showToast('Task deleted', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error deleting';
            showToast(msg, 'error');
        });
    }

    function updateTaskStatus(taskId, newStatus) {
        $.ajax({
            url: '/api/tasks/' + taskId,
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify({ status: newStatus })
        })
        .done(function(task) {
            const idx = tasks.findIndex(t => t.id === task.id);
            if (idx !== -1) tasks[idx] = task;
            renderAllTasks();

            // Check if task was redirected to queue (requested progress but got queued)
            if (newStatus === 'progress' && task.status === 'queued') {
                showToast('Task is running. Added to queue at position ' + task.queue_position, 'info');
            } else if (newStatus === 'progress') {
                openEditTaskModal(task);
            }
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error updating';
            showToast(msg, 'error');
            renderAllTasks();
        });
    }

    function saveSettings() {
        const settingsData = {
            default_project_dir: $('#settingsProjectDir').val().trim(),
            claude_command: $('#settingsClaudeCommand').val().trim(),
            default_max_iterations: parseInt($('#settingsMaxIterations').val()) || 10,
            github_token: $('#settingsGithubToken').val().trim(),
            default_branch: $('#settingsDefaultBranch').val().trim(),
            default_priority: parseInt($('#settingsDefaultPriority').val()) || 2,
            auto_archive_days: parseInt($('#settingsAutoArchive').val()) || 0
        };

        $.ajax({
            url: '/api/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify(settingsData)
        })
        .done(function(data) {
            config = data;
            showToast('Settings saved', 'success');
            closeSettingsModal();
            // Re-check GitHub connection after saving
            checkGithubConnection();
        })
        .fail(function(xhr) {
            showToast('Error saving settings', 'error');
        });
    }

    // Project API Functions
    function saveProject(projectData) {
        const isNew = !projectData.id;
        const url = isNew ? '/api/projects' : '/api/projects/' + projectData.id;
        const method = isNew ? 'POST' : 'PUT';

        $.ajax({
            url: url,
            method: method,
            contentType: 'application/json',
            data: JSON.stringify(projectData)
        })
        .done(function(project) {
            if (isNew) {
                projects.push(project);
            } else {
                const idx = projects.findIndex(p => p.id === project.id);
                if (idx !== -1) projects[idx] = project;
            }
            renderProjectList();
            populateProjectSelect();
            closeProjectModal();
            showToast(isNew ? 'Project created' : 'Project saved', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error saving';
            showToast(msg, 'error');
        });
    }

    function deleteProject(projectId) {
        $.ajax({
            url: '/api/projects/' + projectId,
            method: 'DELETE'
        })
        .done(function() {
            projects = projects.filter(p => p.id !== projectId);
            renderProjectList();
            populateProjectSelect();
            closeProjectModal();
            showToast('Project deleted', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error deleting';
            showToast(msg, 'error');
        });
    }

    function loadBranchRules(projectId) {
        $.get('/api/projects/' + projectId + '/rules')
            .done(function(data) {
                branchRules = data || [];
                renderBranchRules();
            })
            .fail(function(xhr) {
                branchRules = [];
                renderBranchRules();
            });
    }

    function addBranchRule(projectId, pattern) {
        $.ajax({
            url: '/api/projects/' + projectId + '/rules',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ branch_pattern: pattern })
        })
        .done(function(rule) {
            branchRules.push(rule);
            renderBranchRules();
            $('#newBranchRule').val('');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error adding';
            showToast(msg, 'error');
        });
    }

    function deleteBranchRule(ruleId) {
        $.ajax({
            url: '/api/projects/' + currentProjectId + '/rules/' + ruleId,
            method: 'DELETE'
        })
        .done(function() {
            branchRules = branchRules.filter(r => r.id !== ruleId);
            renderBranchRules();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error deleting';
            showToast(msg, 'error');
        });
    }

    // Task Type API Functions
    function saveTaskType(typeData) {
        const isNew = !typeData.id;
        const url = isNew ? '/api/task-types' : '/api/task-types/' + typeData.id;
        const method = isNew ? 'POST' : 'PUT';

        $.ajax({
            url: url,
            method: method,
            contentType: 'application/json',
            data: JSON.stringify(typeData)
        })
        .done(function(taskType) {
            if (isNew) {
                taskTypes.push(taskType);
            } else {
                const idx = taskTypes.findIndex(t => t.id === taskType.id);
                if (idx !== -1) taskTypes[idx] = taskType;
            }
            renderTaskTypeList();
            populateTaskTypeSelect();
            closeTaskTypeModal();
            showToast(isNew ? 'Task type created' : 'Task type saved', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error saving';
            showToast(msg, 'error');
        });
    }

    function deleteTaskType(typeId) {
        $.ajax({
            url: '/api/task-types/' + typeId,
            method: 'DELETE'
        })
        .done(function() {
            taskTypes = taskTypes.filter(t => t.id !== typeId);
            renderTaskTypeList();
            populateTaskTypeSelect();
            closeTaskTypeModal();
            showToast('Task type deleted', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error deleting';
            showToast(msg, 'error');
        });
    }

    // Scan Projects
    function scanProjects(basePath, maxDepth) {
        $('#btnStartScan').prop('disabled', true).text('Scanning...');
        $.ajax({
            url: '/api/projects/scan',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ base_path: basePath, max_depth: maxDepth })
        })
        .done(function(data) {
            scannedRepos = data.projects || [];
            renderScanResults();
            $('#scanResults').removeClass('hidden');
            $('#btnStartScan').addClass('hidden');
            $('#btnImportScan').removeClass('hidden');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error scanning';
            showToast(msg, 'error');
        })
        .always(function() {
            $('#btnStartScan').prop('disabled', false).text('Scan');
        });
    }

    function importScannedProjects() {
        const selected = [];
        $('#scanResultsList input:checked').each(function() {
            selected.push($(this).data('path'));
        });

        if (selected.length === 0) {
            showToast('No repositories selected', 'error');
            return;
        }

        let imported = 0;
        let failed = 0;
        const total = selected.length;

        selected.forEach(function(path) {
            const name = path.split('/').pop();
            $.ajax({
                url: '/api/projects',
                method: 'POST',
                contentType: 'application/json',
                data: JSON.stringify({ name: name, path: path })
            })
            .done(function(project) {
                projects.push(project);
                imported++;
                checkImportComplete();
            })
            .fail(function() {
                failed++;
                checkImportComplete();
            });
        });

        function checkImportComplete() {
            if (imported + failed === total) {
                renderProjectList();
                populateProjectSelect();
                closeScanModal();
                showToast(`${imported} projects imported` + (failed > 0 ? `, ${failed} failed` : ''), imported > 0 ? 'success' : 'error');
            }
        }
    }

    // RALPH Control Functions
    function pauseTask(taskId) {
        $.post('/api/tasks/' + taskId + '/pause')
            .done(function() {
                $('#btnPause').addClass('hidden');
                $('#btnResume').removeClass('hidden');
                showToast('Process paused', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Error pausing';
                showToast(msg, 'error');
            });
    }

    function resumeTask(taskId) {
        $.post('/api/tasks/' + taskId + '/resume')
            .done(function() {
                $('#btnResume').addClass('hidden');
                $('#btnPause').removeClass('hidden');
                showToast('Process resumed', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Error resuming';
                showToast(msg, 'error');
            });
    }

    function stopTask(taskId) {
        $.post('/api/tasks/' + taskId + '/stop')
            .done(function() {
                showToast('Process stopped', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Error stopping';
                showToast(msg, 'error');
            });
    }

    function sendFeedback(taskId, message) {
        $.ajax({
            url: '/api/tasks/' + taskId + '/feedback',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ message: message })
        })
        .done(function() {
            $('#feedbackInput').val('');
            showToast('Feedback sent', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error sending';
            showToast(msg, 'error');
        });
    }

    // Continue task by adding it to queue with a message
    function continueTaskWithMessage(taskId, message) {
        const $btn = $('#btnContinueTask');
        const originalText = $btn.html();
        $btn.addClass('loading').html('<span class="btn-icon">...</span> Queuing...');

        $.ajax({
            url: '/api/tasks/' + taskId + '/continue',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ message: message })
        })
        .done(function(response) {
            $('#continueTaskInput').val('');
            closeModal();
            if (response.queue_position && response.queue_position > 1) {
                showToast('Task added to queue at position ' + response.queue_position, 'success');
            } else {
                showToast('Task resumed', 'success');
            }
            loadTasks(); // Refresh the task list
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error continuing task';
            showToast(msg, 'error');
        })
        .always(function() {
            $btn.removeClass('loading').html(originalText);
        });
    }

    // WebSocket
    function connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(protocol + '//' + window.location.host + '/ws');

        ws.onopen = function() {
            $('#reconnectBanner').addClass('hidden');
            console.log('WebSocket connected');
        };

        ws.onclose = function() {
            $('#reconnectBanner').removeClass('hidden');
            console.log('WebSocket disconnected');
            setTimeout(connectWebSocket, 5000);
        };

        ws.onerror = function(err) {
            console.error('WebSocket error:', err);
        };

        ws.onmessage = function(event) {
            console.log('WS message received:', event.data);
            try {
                const msg = JSON.parse(event.data);
                handleWSMessage(msg);
            } catch (e) {
                console.error('Failed to parse WebSocket message:', e);
            }
        };
    }

    function handleWSMessage(msg) {
        switch (msg.type) {
            case 'log':
                appendLog(msg.task_id, msg.message);
                break;
            case 'status': {
                // Update local task status and re-render if status changed
                const statusTask = tasks.find(t => t.id === msg.task_id);
                if (statusTask && statusTask.status !== msg.status) {
                    statusTask.status = msg.status;
                    if (msg.iteration !== undefined) {
                        statusTask.current_iteration = msg.iteration;
                    }
                    renderAllTasks();
                } else {
                    // Just update badge if status didn't change (e.g., iteration update)
                    updateStatusBadge(msg.task_id, msg.status, msg.iteration);
                }
                break;
            }
            case 'task_updated':
                updateTask(msg.task);
                break;
            case 'project_updated':
                updateProject(msg.project);
                break;
            case 'branch_change':
                updateTaskBranch(msg.task_id, msg.branch);
                break;
            case 'deployment_success':
                showDeploymentSuccess(msg.task_id, msg.message);
                break;
            case 'merge_conflict':
                showMergeConflictModal(msg.conflict);
                break;
        }
    }

    function showDeploymentSuccess(taskId, message) {
        // Find the task and show success animation
        const task = tasks.find(t => t.id === taskId);
        const taskTitle = task ? task.title : 'Task';
        showToast(`Deployed: ${taskTitle}`, 'success');
    }

    function showMergeConflictModal(conflict) {
        if (!conflict) return;

        const task = tasks.find(t => t.id === conflict.task_id);
        const taskTitle = task ? task.title : 'Task';

        // Build file list
        let filesHtml = '';
        if (conflict.files && conflict.files.length > 0) {
            filesHtml = conflict.files.map(f =>
                `<div class="conflict-file"><i class="fas fa-file-code"></i> ${f.path}</div>`
            ).join('');
        } else {
            filesHtml = '<div class="conflict-file"><i class="fas fa-question-circle"></i> Conflict files could not be determined</div>';
        }

        const modalHtml = `
            <div class="modal-overlay" id="conflictModal">
                <div class="modal conflict-modal">
                    <div class="modal-header">
                        <h3><i class="fas fa-exclamation-triangle" style="color: #f0ad4e;"></i> Merge Conflict</h3>
                        <button class="close-btn" onclick="$('#conflictModal').remove()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <p class="conflict-message">
                            The branch <code>${conflict.working_branch}</code> cannot be automatically merged into
                            <code>${conflict.target_branch}</code>.
                        </p>

                        <div class="conflict-task-info">
                            <strong>Task:</strong> ${taskTitle}
                        </div>

                        <div class="conflict-files-section">
                            <h4><i class="fas fa-folder-open"></i> Affected Files:</h4>
                            <div class="conflict-files-list">
                                ${filesHtml}
                            </div>
                        </div>

                        <div class="conflict-actions">
                            <button class="btn btn-primary btn-resolve-ralph" data-task-id="${conflict.task_id}">
                                <i class="fas fa-robot"></i> Let RALPH resolve
                            </button>
                            <button class="btn btn-secondary btn-resolve-manual" data-task-id="${conflict.task_id}">
                                <i class="fas fa-terminal"></i> Resolve manually
                            </button>
                            <button class="btn btn-outline" onclick="$('#conflictModal').remove()">
                                <i class="fas fa-times"></i> Later
                            </button>
                        </div>

                        <div class="conflict-hint">
                            <i class="fas fa-info-circle"></i>
                            <span>RALPH will try to resolve the conflicts intelligently and combine both versions.</span>
                        </div>
                    </div>
                </div>
            </div>
        `;

        // Remove any existing conflict modal
        $('#conflictModal').remove();

        // Add modal to body
        $('body').append(modalHtml);

        // Setup event handlers
        $('.btn-resolve-ralph').click(function() {
            const taskId = $(this).data('task-id');
            resolveConflictWithRalph(taskId);
            $('#conflictModal').remove();
        });

        $('.btn-resolve-manual').click(function() {
            const taskId = $(this).data('task-id');
            showManualResolveInstructions(conflict);
        });

        // Play warning sound or show notification
        showToast('Merge conflict detected!', 'warning');
    }

    function resolveConflictWithRalph(taskId) {
        showToast('RALPH is resolving the conflict...', 'info');

        $.post(`/api/tasks/${taskId}/resolve-conflict`)
            .done(function(data) {
                showToast('RALPH is working on the conflict', 'success');
            })
            .fail(function(xhr) {
                const error = xhr.responseJSON?.error || 'Unknown error';
                showToast(`Error: ${error}`, 'error');
            });
    }

    function showManualResolveInstructions(conflict) {
        const instructions = `
            <div class="manual-resolve-instructions">
                <h4>Manual Conflict Resolution</h4>
                <p>Run the following commands in the terminal:</p>
                <pre><code>cd [project-path]
git checkout ${conflict.working_branch}
git fetch origin
git rebase origin/${conflict.target_branch}
# Resolve the conflicts in the marked files
git add .
git rebase --continue
# If successful, move the task to Done again</code></pre>
            </div>
        `;

        $('.conflict-modal .modal-body').html(instructions +
            '<button class="btn btn-outline" onclick="$(\'#conflictModal\').remove()" style="margin-top: 16px;">Close</button>'
        );
    }

    // ============================================================================
    // STRUCTURED LOG SYSTEM
    // ============================================================================

    // Log state management
    let logState = {
        iterations: [],           // Array of iteration objects
        currentIteration: null,   // Current iteration index
        entries: [],              // All log entries with metadata
        filter: 'all',            // Current filter: 'all', 'errors', 'tools', 'hide-thinking'
        searchQuery: '',          // Current search query
        startTime: null           // When logging started
    };

    // Log entry types with icons and colors
    const LOG_TYPES = {
        system: { icon: '🔔', class: 'log-type-system' },
        init: { icon: '🚀', class: 'log-type-init' },
        thinking: { icon: '💭', class: 'log-type-thinking' },
        tool: { icon: '🔧', class: 'log-type-tool' },
        output: { icon: '📝', class: 'log-type-output' },
        success: { icon: '✅', class: 'log-type-success' },
        error: { icon: '❌', class: 'log-type-error' },
        warning: { icon: '⚠️', class: 'log-type-warning' },
        file: { icon: '📄', class: 'log-type-file' },
        iteration: { icon: '🔄', class: 'log-type-iteration' }
    };

    // Reset log state when opening a new task
    function resetLogState() {
        logState = {
            iterations: [],
            currentIteration: null,
            entries: [],
            filter: 'all',
            searchQuery: '',
            startTime: Date.now()
        };
    }

    // Parse log message and determine type
    function parseLogEntry(message) {
        const timestamp = Date.now();

        // Check for GRINDER system messages
        if (message.startsWith('[GRINDER]')) {
            return {
                type: 'system',
                content: message,
                timestamp,
                raw: message
            };
        }

        // Check for iteration markers
        const iterationMatch = message.match(/\[ITERATION\s+(\d+)\]/i);
        if (iterationMatch) {
            const iterNum = parseInt(iterationMatch[1]);
            return {
                type: 'iteration',
                iterationNumber: iterNum,
                content: message.replace(/\[ITERATION\s+\d+\]/i, '').trim(),
                timestamp,
                raw: message
            };
        }

        // Check for SUCCESS/BLOCKED markers
        if (message.includes('[SUCCESS]')) {
            return {
                type: 'success',
                content: message.replace('[SUCCESS]', '').trim(),
                timestamp,
                raw: message
            };
        }

        if (message.includes('[BLOCKED]')) {
            return {
                type: 'error',
                content: message.replace('[BLOCKED]', '').trim(),
                timestamp,
                raw: message
            };
        }

        // Try to parse as JSON (Claude's structured output)
        try {
            const data = JSON.parse(message.trim());
            return parseJsonLogEntry(data, timestamp, message);
        } catch (e) {
            // Check for error patterns in plain text
            const trimmed = message.trim();
            if (!trimmed) return null;

            if (/error|Error|ERROR|failed|Failed|FAILED|exception|Exception/i.test(trimmed)) {
                return {
                    type: 'error',
                    content: trimmed,
                    timestamp,
                    raw: message
                };
            }

            if (/warning|Warning|WARNING|warn|Warn/i.test(trimmed)) {
                return {
                    type: 'warning',
                    content: trimmed,
                    timestamp,
                    raw: message
                };
            }

            if (/success|Success|SUCCESS|passed|Passed|done|Done|✓|completed/i.test(trimmed)) {
                return {
                    type: 'success',
                    content: trimmed,
                    timestamp,
                    raw: message
                };
            }

            // Default to plain text
            return {
                type: 'text',
                content: trimmed,
                timestamp,
                raw: message
            };
        }
    }

    // Parse JSON log entries from Claude
    function parseJsonLogEntry(data, timestamp, raw) {
        switch (data.type) {
            case 'system':
                if (data.subtype === 'init') {
                    return {
                        type: 'init',
                        content: `Claude started in ${data.cwd}`,
                        cwd: data.cwd,
                        timestamp,
                        raw
                    };
                }
                return null;

            case 'assistant':
                const msg = data.message;
                if (!msg || !msg.content) return null;

                const entries = [];
                for (const block of msg.content) {
                    if (block.type === 'text' && block.text) {
                        entries.push({
                            type: 'thinking',
                            content: block.text,
                            timestamp,
                            raw
                        });
                    }
                    if (block.type === 'tool_use') {
                        const toolEntry = parseToolUse(block, timestamp, raw);
                        if (toolEntry) entries.push(toolEntry);
                    }
                }
                return entries.length === 1 ? entries[0] : (entries.length > 1 ? entries : null);

            case 'user':
                const content = data.message?.content;
                if (!content || !Array.isArray(content)) return null;

                for (const block of content) {
                    if (block.type === 'tool_result') {
                        return parseToolResult(block, timestamp, raw);
                    }
                }
                return null;

            default:
                return null;
        }
    }

    // Parse tool use entries
    function parseToolUse(block, timestamp, raw) {
        const toolName = block.name;
        let toolInfo = '';
        let subtype = 'generic';
        let extraInfo = {};

        switch (toolName) {
            case 'Write':
                toolInfo = block.input?.file_path || '';
                subtype = 'write';
                // Count lines in content being written
                if (block.input?.content) {
                    extraInfo.lineCount = block.input.content.split('\n').length;
                }
                break;
            case 'Edit':
                toolInfo = block.input?.file_path || '';
                subtype = 'edit';
                // Capture old and new string for diff info
                if (block.input?.old_string && block.input?.new_string) {
                    const oldLines = block.input.old_string.split('\n').length;
                    const newLines = block.input.new_string.split('\n').length;
                    extraInfo.linesRemoved = oldLines;
                    extraInfo.linesAdded = newLines;
                    extraInfo.replaceAll = block.input?.replace_all || false;
                }
                break;
            case 'Read':
                toolInfo = block.input?.file_path || '';
                subtype = 'read';
                // Capture offset and limit if specified
                if (block.input?.offset) extraInfo.offset = block.input.offset;
                if (block.input?.limit) extraInfo.limit = block.input.limit;
                break;
            case 'Bash':
                toolInfo = block.input?.command || '';
                subtype = 'bash';
                return {
                    type: 'tool',
                    toolName,
                    toolInfo,
                    subtype,
                    command: block.input?.command,
                    description: block.input?.description,
                    timestamp,
                    raw
                };
            case 'TodoWrite':
                toolInfo = 'Updating task list...';
                subtype = 'todo';
                // Count todos if available
                if (block.input?.todos && Array.isArray(block.input.todos)) {
                    extraInfo.todoCount = block.input.todos.length;
                    extraInfo.inProgress = block.input.todos.filter(t => t.status === 'in_progress').length;
                    extraInfo.completed = block.input.todos.filter(t => t.status === 'completed').length;
                }
                break;
            case 'Glob':
                toolInfo = block.input?.pattern || '';
                subtype = 'search';
                if (block.input?.path) extraInfo.searchPath = block.input.path;
                break;
            case 'Grep':
                toolInfo = block.input?.pattern || '';
                subtype = 'search';
                if (block.input?.path) extraInfo.searchPath = block.input.path;
                if (block.input?.type) extraInfo.fileType = block.input.type;
                break;
            case 'WebFetch':
                toolInfo = block.input?.url || '';
                subtype = 'web';
                break;
            case 'WebSearch':
                toolInfo = block.input?.query || '';
                subtype = 'web';
                break;
            case 'Task':
                toolInfo = block.input?.description || 'Subtask';
                subtype = 'task';
                if (block.input?.subagent_type) extraInfo.agentType = block.input.subagent_type;
                break;
            case 'LSP':
                toolInfo = block.input?.operation || '';
                subtype = 'lsp';
                if (block.input?.filePath) extraInfo.lspFile = block.input.filePath;
                break;
            default:
                toolInfo = JSON.stringify(block.input || {}).substring(0, 100);
        }

        return {
            type: 'tool',
            toolName,
            toolInfo,
            subtype,
            filePath: block.input?.file_path,
            extraInfo,
            timestamp,
            raw
        };
    }

    // Parse tool result entries
    function parseToolResult(block, timestamp, raw) {
        const result = block.content || '';
        const isError = block.is_error;
        const lines = result.split('\n').length;

        if (isError) {
            return {
                type: 'error',
                content: result,
                isToolResult: true,
                lineCount: lines,
                timestamp,
                raw
            };
        }

        // Detect exit codes in output
        const exitCodeMatch = result.match(/exit code:?\s*(\d+)/i);
        const exitCode = exitCodeMatch ? parseInt(exitCodeMatch[1]) : null;

        return {
            type: 'output',
            content: result,
            isToolResult: true,
            lineCount: lines,
            exitCode,
            isSuccess: exitCode === 0 || (!exitCode && !isError),
            timestamp,
            raw
        };
    }

    // Render a single log entry
    function renderLogEntry(entry, isNew = false) {
        if (!entry) return '';

        // Handle array of entries
        if (Array.isArray(entry)) {
            return entry.map(e => renderLogEntry(e, isNew)).join('');
        }

        const typeInfo = LOG_TYPES[entry.type] || { icon: '•', class: '' };
        const newClass = isNew ? ' new-line' : '';
        const timestamp = formatRelativeTime(entry.timestamp);
        const timestampTitle = new Date(entry.timestamp).toLocaleString();

        let contentHtml = '';

        switch (entry.type) {
            case 'system':
                contentHtml = `<span class="log-content">${escapeHtml(entry.content)}</span>`;
                break;

            case 'init':
                contentHtml = `<span class="log-content">Claude started in <code>${escapeHtml(entry.cwd)}</code></span>`;
                break;

            case 'thinking':
                contentHtml = `<span class="log-content">${escapeHtml(entry.content)}</span>`;
                break;

            case 'tool':
                contentHtml = renderToolEntry(entry);
                break;

            case 'output':
                return renderOutputEntry(entry, isNew, timestamp, timestampTitle);

            case 'success':
                contentHtml = `<span class="log-content">${escapeHtml(entry.content)}</span>`;
                break;

            case 'error':
                contentHtml = `<span class="log-content">${escapeHtml(entry.content)}</span>`;
                break;

            case 'warning':
                contentHtml = `<span class="log-content">${escapeHtml(entry.content)}</span>`;
                break;

            case 'iteration':
                // Iterations are handled separately
                return '';

            default:
                contentHtml = `<span class="log-content">${escapeHtml(entry.content || '')}</span>`;
        }

        return `
            <div class="log-entry ${typeInfo.class}${newClass}" data-type="${entry.type}" data-timestamp="${entry.timestamp}">
                <span class="log-icon">${typeInfo.icon}</span>
                ${contentHtml}
                <span class="log-timestamp" title="${timestampTitle}">${timestamp}</span>
            </div>
        `;
    }

    // Render tool entry
    function renderToolEntry(entry) {
        let html = `<span class="log-content">`;
        html += `<span class="tool-badge">${entry.toolName}</span>`;
        const extra = entry.extraInfo || {};

        if (entry.subtype === 'bash' && entry.command) {
            // Bash command with description if available
            html += `
                <div class="command-block">
                    <div class="command-header">
                        <span class="command-prompt">$</span>
                        <span class="command-text">${escapeHtml(entry.command)}</span>
                    </div>
                    ${entry.description ? `<div class="command-description">${escapeHtml(entry.description)}</div>` : ''}
                </div>
            `;
        } else if (entry.subtype === 'write' && entry.filePath) {
            // Write file operation
            html += `<span class="file-path">${escapeHtml(entry.filePath)}</span>`;
            if (extra.lineCount) {
                html += `<span class="file-info">(${extra.lineCount} lines)</span>`;
            }
        } else if (entry.subtype === 'edit' && entry.filePath) {
            // Edit file operation
            html += `<span class="file-path">${escapeHtml(entry.filePath)}</span>`;
            if (extra.linesRemoved !== undefined && extra.linesAdded !== undefined) {
                const diff = extra.linesAdded - extra.linesRemoved;
                const diffText = diff > 0 ? `+${diff}` : (diff < 0 ? `${diff}` : '±0');
                html += `<span class="file-info edit-info">(${diffText} lines${extra.replaceAll ? ', all' : ''})</span>`;
            }
        } else if (entry.subtype === 'read' && entry.filePath) {
            // Read file operation
            html += `<span class="file-path">${escapeHtml(entry.filePath)}</span>`;
            if (extra.offset || extra.limit) {
                const rangeInfo = [];
                if (extra.offset) rangeInfo.push(`offset: ${extra.offset}`);
                if (extra.limit) rangeInfo.push(`limit: ${extra.limit}`);
                html += `<span class="file-info">(${rangeInfo.join(', ')})</span>`;
            }
        } else if (entry.subtype === 'todo' && extra.todoCount !== undefined) {
            // Todo write with status
            html += `<span class="tool-info">${extra.todoCount} tasks (${extra.completed || 0} done, ${extra.inProgress || 0} active)</span>`;
        } else if (entry.subtype === 'search') {
            // Search operation (Glob/Grep)
            html += `<span class="search-pattern">"${escapeHtml(entry.toolInfo)}"</span>`;
            if (extra.searchPath) {
                html += `<span class="file-info">in ${escapeHtml(extra.searchPath)}</span>`;
            }
            if (extra.fileType) {
                html += `<span class="file-info">(${extra.fileType} files)</span>`;
            }
        } else if (entry.subtype === 'web') {
            // Web operations
            html += `<span class="tool-info web-url">${escapeHtml(entry.toolInfo)}</span>`;
        } else if (entry.subtype === 'task') {
            // Task agent
            html += `<span class="tool-info">${escapeHtml(entry.toolInfo)}</span>`;
            if (extra.agentType) {
                html += `<span class="agent-badge">${escapeHtml(extra.agentType)}</span>`;
            }
        } else if (entry.subtype === 'lsp') {
            // LSP operation
            html += `<span class="tool-info">${escapeHtml(entry.toolInfo)}</span>`;
            if (extra.lspFile) {
                html += `<span class="file-path">${escapeHtml(extra.lspFile)}</span>`;
            }
        } else if (entry.filePath) {
            html += `<span class="file-path">${escapeHtml(entry.filePath)}</span>`;
        } else if (entry.toolInfo) {
            html += `<span class="tool-info">${escapeHtml(entry.toolInfo)}</span>`;
        }

        html += `</span>`;
        return html;
    }

    // Render output entry with collapsible content
    function renderOutputEntry(entry, isNew, timestamp, timestampTitle) {
        const typeInfo = LOG_TYPES.output;
        const newClass = isNew ? ' new-line' : '';
        const lines = entry.content.split('\n');
        const lineCount = lines.length;
        const isLong = lineCount > 10;
        const previewContent = isLong ? lines.slice(0, 10).join('\n') : entry.content;
        const exitCodeHtml = entry.exitCode !== null
            ? `<span class="exit-code ${entry.exitCode === 0 ? 'success' : 'error'}">${entry.exitCode === 0 ? '✓' : '✗'} Exit: ${entry.exitCode}</span>`
            : '';

        return `
            <div class="log-entry log-type-output${newClass}" data-type="output" data-timestamp="${entry.timestamp}">
                <span class="log-icon">${typeInfo.icon}</span>
                <span class="log-content">
                    <div class="output-block">
                        <div class="output-header">
                            <span class="output-line-count">${lineCount} line${lineCount !== 1 ? 's' : ''}</span>
                            ${isLong ? '<button class="show-more-btn" onclick="toggleOutputExpand(this)">Show more ▼</button>' : ''}
                        </div>
                        <div class="output-content${isLong ? ' collapsed' : ''}" data-full="${escapeHtml(entry.content)}">${escapeHtml(isLong ? previewContent : entry.content)}</div>
                        ${exitCodeHtml}
                    </div>
                </span>
                <span class="log-timestamp" title="${timestampTitle}">${timestamp}</span>
            </div>
        `;
    }

    // Format relative time
    function formatRelativeTime(timestamp) {
        const now = Date.now();
        const diff = Math.floor((now - timestamp) / 1000);

        if (diff < 5) return 'now';
        if (diff < 60) return `${diff}s ago`;
        if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
        if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
        return `${Math.floor(diff / 86400)}d ago`;
    }

    // Create or update iteration block
    function ensureIteration(iterNum, summary = '') {
        let iteration = logState.iterations.find(i => i.number === iterNum);

        if (!iteration) {
            iteration = {
                number: iterNum,
                summary: summary,
                status: 'running',
                startTime: Date.now(),
                endTime: null,
                entries: []
            };
            logState.iterations.push(iteration);
            logState.currentIteration = iterNum;

            // Collapse previous iterations
            logState.iterations.forEach(i => {
                if (i.number < iterNum) {
                    i.collapsed = true;
                }
            });
        }

        return iteration;
    }

    // Generate a summary for an iteration based on its entries
    function generateIterationSummary(iteration) {
        if (iteration.summary) return iteration.summary;

        const entries = iteration.entries;
        if (!entries || entries.length === 0) return '';

        // Find the first thinking entry for context
        const thinkingEntry = entries.find(e => e.type === 'thinking');
        if (thinkingEntry && thinkingEntry.content) {
            // Extract first sentence or truncate
            const text = thinkingEntry.content;
            const firstSentence = text.split(/[.!?\n]/)[0];
            if (firstSentence && firstSentence.length > 5) {
                return firstSentence.length > 60 ? firstSentence.substring(0, 57) + '...' : firstSentence;
            }
        }

        // Fallback: summarize based on tool usage
        const toolEntries = entries.filter(e => e.type === 'tool');
        if (toolEntries.length > 0) {
            const toolCounts = {};
            toolEntries.forEach(e => {
                const name = e.toolName || 'Tool';
                toolCounts[name] = (toolCounts[name] || 0) + 1;
            });

            const toolSummary = Object.entries(toolCounts)
                .map(([name, count]) => count > 1 ? `${name} (${count}x)` : name)
                .slice(0, 3)
                .join(', ');

            return toolSummary;
        }

        return '';
    }

    // Render all iterations
    function renderIterations() {
        const $log = $('#logOutput');
        let html = '';

        if (logState.iterations.length === 0) {
            // No iterations yet, render entries directly
            const entriesHtml = logState.entries.map(e => renderLogEntry(e)).join('');
            $log.html(entriesHtml || '<span class="waiting">Claude is starting... waiting for output...</span>');
            return;
        }

        // Get total iteration count (for display like "Iteration 3/10")
        const totalIterations = logState.iterations.length;

        // Render each iteration as a collapsible block
        for (const iteration of logState.iterations) {
            const isCollapsed = iteration.collapsed && iteration.status !== 'running';
            const duration = formatDuration(iteration.startTime, iteration.endTime || Date.now());
            const statusClass = iteration.status === 'running' ? 'running' :
                               (iteration.status === 'error' ? 'error' : 'completed');
            const statusIcon = iteration.status === 'running' ? '● Running' :
                              (iteration.status === 'error' ? '✗ Error' : '✓');

            // Generate summary if not provided
            const summary = generateIterationSummary(iteration);

            // Count stats for this iteration
            const toolCount = iteration.entries.filter(e => e.type === 'tool').length;
            const errorCount = iteration.entries.filter(e => e.type === 'error').length;

            html += `
                <div class="iteration-block${isCollapsed ? ' collapsed' : ''}" data-iteration="${iteration.number}">
                    <div class="iteration-header" onclick="toggleIteration(${iteration.number})">
                        <span class="iteration-toggle">▼</span>
                        <span class="iteration-title">Iteration ${iteration.number}</span>
                        <span class="iteration-summary">${escapeHtml(summary)}</span>
                        <span class="iteration-stats">
                            ${toolCount > 0 ? `<span class="stat-tool" title="${toolCount} tool calls">🔧${toolCount}</span>` : ''}
                            ${errorCount > 0 ? `<span class="stat-error" title="${errorCount} errors">❌${errorCount}</span>` : ''}
                        </span>
                        <span class="iteration-status ${statusClass}">${statusIcon}</span>
                        <span class="iteration-duration">${duration}</span>
                    </div>
                    <div class="iteration-content">
                        ${iteration.entries.map(e => renderLogEntry(e)).join('')}
                        ${iteration.status === 'running' ? '<div class="thinking-indicator"><span class="pulsing-dot"></span> Processing...</div>' : ''}
                    </div>
                </div>
            `;
        }

        $log.html(html);
    }

    // Format duration
    function formatDuration(start, end) {
        const diff = Math.floor((end - start) / 1000);
        if (diff < 60) return `${diff}s`;
        const mins = Math.floor(diff / 60);
        const secs = diff % 60;
        return `${mins}m ${secs}s`;
    }

    // Append new log entry
    function appendLog(taskId, message) {
        if (currentTaskId !== taskId) return;

        const entry = parseLogEntry(message);
        if (!entry) return;

        // Handle array of entries
        const entries = Array.isArray(entry) ? entry : [entry];

        for (const e of entries) {
            // Handle iteration markers
            if (e.type === 'iteration') {
                ensureIteration(e.iterationNumber, e.content);
                continue;
            }

            // Add entry to current iteration or global list
            if (logState.currentIteration !== null) {
                const iteration = logState.iterations.find(i => i.number === logState.currentIteration);
                if (iteration) {
                    iteration.entries.push(e);

                    // Update iteration status on success/error
                    if (e.type === 'success') {
                        iteration.status = 'completed';
                        iteration.endTime = Date.now();
                    } else if (e.type === 'error') {
                        iteration.status = 'error';
                        iteration.endTime = Date.now();
                    }
                }
            } else {
                logState.entries.push(e);
            }
        }

        // Re-render and scroll
        renderIterations();
        applyLogFilters();

        if (autoScroll) {
            scrollToBottom($('#logOutput'));
        }
    }

    // Scroll to bottom using requestAnimationFrame for reliability
    function scrollToBottom($log) {
        if (!$log || !$log.length) return;

        // Mark as programmatic scroll to prevent triggering manual scroll detection
        isProgrammaticScroll = true;

        // Use requestAnimationFrame to ensure DOM is updated before scrolling
        requestAnimationFrame(function() {
            const element = $log[0];
            element.scrollTop = element.scrollHeight;

            // Reset flag after scroll completes
            requestAnimationFrame(function() {
                isProgrammaticScroll = false;
            });
        });
    }

    // Check if user is near the bottom of the log (within threshold)
    function isNearBottom($log) {
        if (!$log || !$log.length) return true;
        const element = $log[0];
        const threshold = 50; // pixels from bottom
        return (element.scrollHeight - element.scrollTop - element.clientHeight) <= threshold;
    }

    // Setup scroll detection for manual scrolling
    function setupLogScrollDetection() {
        const $log = $('#logOutput');

        $log.off('scroll.autoScroll').on('scroll.autoScroll', function() {
            // Ignore programmatic scrolls
            if (isProgrammaticScroll) return;

            // Clear previous timeout
            if (scrollTimeout) {
                clearTimeout(scrollTimeout);
            }

            // Debounce to detect when scroll stops
            scrollTimeout = setTimeout(function() {
                // If user scrolled away from bottom while auto-scroll is on, disable it
                if (autoScroll && !isNearBottom($log)) {
                    autoScroll = false;
                    $('#autoScroll').prop('checked', false);
                }
            }, 150);
        });
    }

    // Apply log filters and search
    function applyLogFilters() {
        const filter = logState.filter;
        const searchQuery = logState.searchQuery.toLowerCase();

        $('.log-entry').each(function() {
            const $entry = $(this);
            const type = $entry.data('type');
            let visible = true;

            // Apply filter
            switch (filter) {
                case 'errors':
                    visible = type === 'error' || type === 'warning';
                    break;
                case 'tools':
                    visible = type === 'tool' || type === 'output';
                    break;
                case 'hide-thinking':
                    visible = type !== 'thinking';
                    break;
                default:
                    visible = true;
            }

            // Apply search
            if (visible && searchQuery) {
                const content = $entry.text().toLowerCase();
                visible = content.includes(searchQuery);
                $entry.toggleClass('search-match', visible);
            } else {
                $entry.removeClass('search-match');
            }

            $entry.toggleClass('filtered-out', !visible);
        });
    }

    // Timestamp update interval
    let timestampUpdateInterval = null;

    // Update all visible timestamps
    function updateTimestamps() {
        $('.log-entry .log-timestamp').each(function() {
            const $ts = $(this);
            const timestamp = $ts.closest('.log-entry').data('timestamp');
            if (timestamp) {
                $ts.text(formatRelativeTime(timestamp));
            }
        });

        // Also update iteration durations
        $('.iteration-block').each(function() {
            const $block = $(this);
            const iterNum = $block.data('iteration');
            const iteration = logState.iterations.find(i => i.number === iterNum);
            if (iteration) {
                const duration = formatDuration(iteration.startTime, iteration.endTime || Date.now());
                $block.find('.iteration-duration').text(duration);
            }
        });
    }

    // Start timestamp update interval
    function startTimestampUpdates() {
        stopTimestampUpdates();
        timestampUpdateInterval = setInterval(updateTimestamps, 5000); // Update every 5 seconds
    }

    // Stop timestamp update interval
    function stopTimestampUpdates() {
        if (timestampUpdateInterval) {
            clearInterval(timestampUpdateInterval);
            timestampUpdateInterval = null;
        }
    }

    // Setup filter buttons
    function setupLogFilters() {
        $('.filter-btn').off('click').on('click', function() {
            const filter = $(this).data('filter');

            // Toggle active state
            if (filter === 'hide-thinking') {
                // Toggle behavior for hide-thinking
                $(this).toggleClass('active');
                logState.filter = $(this).hasClass('active') ? 'hide-thinking' : 'all';
                // Deactivate other filters when hide-thinking is active
                if (logState.filter === 'hide-thinking') {
                    $('.filter-btn').not(this).removeClass('active');
                } else {
                    $('.filter-btn[data-filter="all"]').addClass('active');
                }
            } else {
                // Single select for other filters
                $('.filter-btn').removeClass('active');
                $(this).addClass('active');
                logState.filter = filter;
            }

            applyLogFilters();
        });

        // Setup search
        let searchTimeout;
        $('#logSearch').off('input').on('input', function() {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(function() {
                logState.searchQuery = $('#logSearch').val();
                applyLogFilters();
            }, 200);
        });
    }

    // Parse existing logs when loading a task
    function parseExistingLogs(logsText) {
        resetLogState();

        if (!logsText) return;

        // Split by lines and parse each
        const lines = logsText.split('\n');
        for (const line of lines) {
            if (!line.trim()) continue;

            const entry = parseLogEntry(line);
            if (!entry) continue;

            // Handle array of entries
            const entries = Array.isArray(entry) ? entry : [entry];

            for (const e of entries) {
                if (e.type === 'iteration') {
                    ensureIteration(e.iterationNumber, e.content);
                } else if (logState.currentIteration !== null) {
                    const iteration = logState.iterations.find(i => i.number === logState.currentIteration);
                    if (iteration) {
                        iteration.entries.push(e);
                    }
                } else {
                    logState.entries.push(e);
                }
            }
        }

        renderIterations();
    }

    // Legacy compatibility - formatLogMessage for backwards compatibility
    function formatLogMessage(message) {
        const entry = parseLogEntry(message);
        if (!entry) return null;
        return renderLogEntry(entry);
    }

    // Legacy compatibility - formatJsonLog
    function formatJsonLog(data) {
        const entry = parseJsonLogEntry(data, Date.now(), '');
        if (!entry) return null;
        return renderLogEntry(entry);
    }

    function updateStatusBadge(taskId, status, iteration) {
        const $card = $(`.task-card[data-id="${taskId}"]`);
        let $badge = $card.find('.status-badge');
        const $footer = $card.find('.task-card-footer');

        // Build new badge based on status
        let newBadgeHtml = '';
        let newBadgeClass = '';
        let newBadgeText = '';

        if (status === 'progress') {
            newBadgeClass = 'running';
            newBadgeText = iteration > 0 ? `Iteration ${iteration}` : 'Running...';
        } else if (status === 'queued') {
            // Queue position badge handled separately in createTaskCard
            newBadgeClass = 'queued';
            newBadgeText = 'QUEUED';
        } else if (status === 'review') {
            newBadgeClass = 'review';
            newBadgeText = 'REVIEW';
        } else if (status === 'done') {
            newBadgeClass = 'done';
            newBadgeText = 'DONE';
        } else if (status === 'blocked') {
            newBadgeClass = 'blocked';
            newBadgeText = 'BLOCKED';
        }

        // Update or create badge
        if (newBadgeClass) {
            if ($badge.length === 0) {
                $footer.prepend(`<span class="status-badge ${newBadgeClass}">${newBadgeText}</span>`);
            } else {
                $badge.removeClass('running queued review done blocked')
                      .addClass(newBadgeClass)
                      .text(newBadgeText);
            }
        } else if ($badge.length > 0) {
            // No badge needed (e.g., backlog) - remove existing
            $badge.remove();
        }

        // Update modal badge if this task is open
        if (currentTaskId === taskId && status === 'progress') {
            $('#iterationBadge').text('Iteration ' + iteration);
        }
    }

    function updateTask(task) {
        const idx = tasks.findIndex(t => t.id === task.id);
        if (idx !== -1) {
            tasks[idx] = task;
        } else {
            tasks.push(task);
        }
        renderAllTasks();

        if (currentTaskId === task.id) {
            updateModalForTask(task);
        }
    }

    function updateProject(project) {
        const idx = projects.findIndex(p => p.id === project.id);
        if (idx !== -1) {
            projects[idx] = project;
        } else {
            projects.push(project);
        }
        renderProjectList();
        populateProjectSelect();
    }

    function updateTaskBranch(taskId, branch) {
        const task = tasks.find(t => t.id === taskId);
        if (task) {
            task.working_branch = branch;
            renderAllTasks();
        }

        if (currentTaskId === taskId) {
            $('#taskBranch').text(branch || '-');
            if (branch) {
                $('#branchInfoGroup').removeClass('hidden');
            }
        }
    }

    // Update logo icon pulsating state based on active tasks
    function updateLogoIconState() {
        const $logoIcon = $('#logoIcon');
        const hasActiveTasks = tasks.some(t => t.status === 'progress');

        if (hasActiveTasks) {
            $logoIcon.addClass('pulsating');
        } else {
            $logoIcon.removeClass('pulsating');
        }
    }

    // Rendering
    function renderAllTasks() {
        const statuses = ['backlog', 'queued', 'progress', 'review', 'done', 'blocked'];

        // Update logo icon pulsating state
        updateLogoIconState();

        statuses.forEach(function(status) {
            const $container = $(`.column[data-status="${status}"] .tasks-container`);
            $container.empty();

            let statusTasks = tasks.filter(t => t.status === status);

            // Filter by project if selected
            if (selectedProjectFilter) {
                statusTasks = statusTasks.filter(t => t.project_id === selectedProjectFilter);
            }

            // Sort queued tasks by queue position
            if (status === 'queued') {
                statusTasks.sort((a, b) => (a.queue_position || 0) - (b.queue_position || 0));
            }

            statusTasks.forEach(function(task) {
                $container.append(createTaskCard(task));
            });

            // Update mobile tab counts
            $(`.mobile-tab-count[data-count="${status}"]`).text(statusTasks.length);
        });
    }

    // ============================================================================
    // Mobile Tab Navigation
    // ============================================================================

    function setupMobileTabNavigation() {
        // Handle mobile tab clicks
        $(document).on('click', '.mobile-tab', function() {
            const status = $(this).data('status');
            switchMobileTab(status);
        });

        // Initialize: ensure backlog is active
        switchMobileTab('backlog');
    }

    function switchMobileTab(status) {
        activeMobileTab = status;

        // Update tab active states
        $('.mobile-tab').removeClass('active');
        $(`.mobile-tab[data-status="${status}"]`).addClass('active');

        // Update column visibility
        $('.column').removeClass('mobile-active');
        $(`.column[data-status="${status}"]`).addClass('mobile-active');

        // Scroll active tab into view
        const $activeTab = $(`.mobile-tab[data-status="${status}"]`);
        if ($activeTab.length) {
            const container = document.getElementById('mobileColumnTabs');
            const tab = $activeTab[0];
            if (container && tab) {
                const containerRect = container.getBoundingClientRect();
                const tabRect = tab.getBoundingClientRect();

                if (tabRect.left < containerRect.left || tabRect.right > containerRect.right) {
                    tab.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' });
                }
            }
        }
    }

    // Check if we're on mobile (matches CSS breakpoint)
    function isMobileView() {
        return window.innerWidth < 768;
    }

    // Build dropdown menu items based on task state
    function buildTaskDropdownItems(task) {
        const items = [];
        // Trunk-based development: Merge option removed
        // Dropdown now empty - but kept for potential future actions
        return items;
    }

    function createTaskCard(task) {
        const taskType = task.task_type || taskTypes.find(t => t.id === task.task_type_id);
        const typeBadge = taskType ?
            `<span class="task-type-badge" style="background-color: ${taskType.color}">${escapeHtml(taskType.name)}</span>` : '';

        // Build status badge HTML
        let statusBadge = '';
        if (task.status === 'progress') {
            const badgeText = task.current_iteration > 0
                ? `Iteration ${task.current_iteration}`
                : 'Running...';
            statusBadge = `<span class="status-badge running">${badgeText}</span>`;
        } else if (task.status === 'queued') {
            const queuePos = task.queue_position || '?';
            statusBadge = `<span class="status-badge queued"><span class="queue-position-badge">${queuePos}</span> QUEUED</span>`;
        } else if (task.status === 'review') {
            statusBadge = `<span class="status-badge review">REVIEW</span>`;
        } else if (task.status === 'done') {
            statusBadge = `<span class="status-badge done">DONE</span>`;
        } else if (task.status === 'blocked') {
            statusBadge = `<span class="status-badge blocked">BLOCKED</span>`;
        }

        // Trunk-based development: Branch row removed - all tasks work on the same branch

        // Build rollback button for review/blocked tasks with rollback_tag
        let rollbackButtonHtml = '';
        if (task.rollback_tag && (task.status === 'review' || task.status === 'blocked')) {
            rollbackButtonHtml = `
                <button class="btn-rollback" title="Rollback changes">
                    <svg viewBox="0 0 16 16" fill="currentColor">
                        <path d="M9.78 12.78a.75.75 0 0 1-1.06 0L4.47 8.53a.75.75 0 0 1 0-1.06l4.25-4.25a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042L6.06 8l3.72 3.72a.75.75 0 0 1 0 1.06Z"/>
                    </svg>
                    Rollback
                </button>`;
        }

        // Build dropdown menu items
        const dropdownItems = buildTaskDropdownItems(task);
        const dropdownHtml = dropdownItems.length > 0 ? `
            <div class="task-card-actions">
                <button class="task-dropdown-toggle" data-id="${task.id}">
                    <svg viewBox="0 0 16 16" fill="currentColor">
                        <path d="M8 9a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3ZM1.5 9a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Zm13 0a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z"/>
                    </svg>
                </button>
                <div class="task-dropdown-menu" data-task-id="${task.id}">
                    ${dropdownItems.join('')}
                </div>
            </div>
        ` : '';

        // Build the card with new layout structure
        // - Header: priority + title
        // - Badge row: type badge (left) + status badge (right)
        // - Footer: LIVE button, rollback button, and attachment badge
        const badgeRowHtml = (typeBadge || statusBadge) ?
            `<div class="task-card-badges">${typeBadge}<div class="badge-spacer"></div>${statusBadge}</div>` : '';

        const $card = $(`
            <div class="task-card" data-id="${task.id}" draggable="true">
                ${dropdownHtml}
                <div class="task-card-header">
                    <div class="priority-indicator priority-${task.priority}"></div>
                    <span class="task-title">${escapeHtml(task.title)}</span>
                </div>
                ${badgeRowHtml}
                <div class="task-card-footer"></div>
            </div>
        `);

        // Add LIVE button for running tasks
        if (task.status === 'progress') {
            $card.find('.task-card-footer').append(
                `<button class="btn-live" data-id="${task.id}">LIVE</button>`
            );
        }

        // Add rollback button for review/blocked tasks with rollback tag (Trunk-based development)
        if (rollbackButtonHtml) {
            $card.find('.task-card-footer').append(rollbackButtonHtml);
        }

        // Show attachment badge if task has attachments
        if (task.attachments && task.attachments.length > 0) {
            $card.find('.task-card-footer').append(`
                <span class="attachment-badge" title="${task.attachments.length} attachment${task.attachments.length > 1 ? 's' : ''}">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>
                    </svg>
                    ${task.attachments.length}
                </span>
            `);
        }

        // Hide footer if empty
        if ($card.find('.task-card-footer').children().length === 0) {
            $card.find('.task-card-footer').addClass('hidden');
        }

        // Show conflict section if task has a conflict PR open
        if (task.working_branch && task.conflict_pr_url && task.conflict_pr_number) {
            $card.append(`
                <div class="merge-conflict-section">
                    <div class="merge-conflict-header">
                        <span class="conflict-icon">&#9888;</span>
                        <span class="conflict-text">Conflict</span>
                    </div>
                    <a href="${escapeHtml(task.conflict_pr_url)}" target="_blank" class="btn-resolve-github">
                        &#128279; Resolve in GitHub
                    </a>
                    <span class="pr-number">#${task.conflict_pr_number}</span>
                </div>
            `);
        }

        return $card;
    }

    function renderProjectList() {
        const $list = $('.project-list');
        // Keep "All Projects" item, remove others
        $list.find('.project-item:not([data-project-id=""]), .project-folder').remove();

        if (projects.length === 0) {
            return;
        }

        // Build tree structure from project paths
        const tree = buildProjectTree(projects);

        // Render the tree
        renderProjectTree($list, tree);

        // Update active state
        $('.project-item').removeClass('active');
        $(`.project-item[data-project-id="${selectedProjectFilter}"]`).addClass('active');
    }

    // Build a tree structure from project paths
    function buildProjectTree(projectList) {
        const root = { children: {}, projects: [] };

        projectList.forEach(function(project) {
            const parts = project.path.split('/').filter(p => p);
            let current = root;

            // Navigate/create path in tree (all but last segment)
            const folderParts = parts.slice(0, -1);
            folderParts.forEach(function(part, idx) {
                if (!current.children[part]) {
                    current.children[part] = {
                        name: part,
                        path: '/' + parts.slice(0, idx + 1).join('/'),
                        children: {},
                        projects: []
                    };
                }
                current = current.children[part];
            });

            // Add project to the deepest folder
            current.projects.push(project);
        });

        // Simplify the tree by collapsing single-child chains
        return simplifyTree(root);
    }

    // Collapse single-child folder chains into combined paths
    function simplifyTree(node) {
        const newChildren = {};

        Object.keys(node.children).forEach(function(key) {
            let child = node.children[key];
            let combinedName = child.name;
            let combinedPath = child.path;

            // Keep collapsing while there's only one child folder and no projects
            while (Object.keys(child.children).length === 1 && child.projects.length === 0) {
                const onlyChildKey = Object.keys(child.children)[0];
                const onlyChild = child.children[onlyChildKey];
                combinedName += '/' + onlyChild.name;
                combinedPath = onlyChild.path;
                child = onlyChild;
            }

            // Recursively simplify children
            const simplifiedChild = simplifyTree(child);
            simplifiedChild.name = combinedName;
            simplifiedChild.path = combinedPath;
            newChildren[combinedPath] = simplifiedChild;
        });

        node.children = newChildren;
        return node;
    }

    // Render tree recursively
    function renderProjectTree($container, node, depth) {
        depth = depth || 0;

        // Sort folder keys
        const folderKeys = Object.keys(node.children).sort();

        folderKeys.forEach(function(key) {
            const folder = node.children[key];
            const folderId = 'folder-' + btoa(folder.path).replace(/[^a-zA-Z0-9]/g, '');
            const isCollapsed = collapsedFolders[folder.path] !== false; // Default to collapsed
            const projectCount = countProjectsInFolder(folder);

            const $folder = $(`
                <div class="project-folder" data-folder-path="${escapeHtml(folder.path)}">
                    <div class="project-folder-header" data-folder-id="${folderId}">
                        <span class="folder-toggle ${isCollapsed ? 'collapsed' : ''}">&#9660;</span>
                        <span class="folder-header-icon">&#128193;</span>
                        <span class="folder-header-name" title="${escapeHtml(folder.path)}">${escapeHtml(folder.name)}</span>
                        <span class="folder-project-count">${projectCount}</span>
                    </div>
                    <div class="project-folder-children ${isCollapsed ? 'collapsed' : ''}" id="${folderId}">
                    </div>
                </div>
            `);

            $container.append($folder);

            const $children = $folder.find('.project-folder-children');

            // Render nested folders
            renderProjectTree($children, folder, depth + 1);

            // Render projects in this folder
            folder.projects.forEach(function(project) {
                const branchHtml = project.current_branch ?
                    `<span class="project-branch">${escapeHtml(project.current_branch)}</span>` : '';
                const icon = project.is_git_repo ? '&#128193;' : '&#128194;';
                const taskCount = tasks.filter(t => t.project_id === project.id).length;
                const countHtml = taskCount > 0 ? `<span class="project-task-count">${taskCount}</span>` : '';

                // Git status badge
                const gitBadge = project.is_git_repo
                    ? '<span class="project-git-badge git">Git</span>'
                    : '<span class="project-git-badge no-git">No Git</span>';

                // Action buttons
                let actionsHtml = '<div class="project-actions">';
                if (!project.is_git_repo) {
                    actionsHtml += '<button class="btn btn-small btn-secondary btn-init-git" title="Initialize Git">Init</button>';
                } else {
                    actionsHtml += '<button class="btn btn-small btn-secondary btn-create-repo" title="Create GitHub Repo">+GH</button>';
                }
                actionsHtml += '</div>';

                $children.append(`
                    <div class="project-item" data-project-id="${project.id}">
                        <span class="project-icon">${icon}</span>
                        <span class="project-name" title="${escapeHtml(project.name)}">${escapeHtml(project.name)}</span>
                        ${gitBadge}
                        ${branchHtml}
                        ${countHtml}
                        ${actionsHtml}
                    </div>
                `);
            });

            // Set max-height for animation
            if (!isCollapsed) {
                $children.css('max-height', $children[0].scrollHeight + 'px');
            }
        });

        // Render projects at root level (if any)
        if (depth === 0) {
            node.projects.forEach(function(project) {
                const branchHtml = project.current_branch ?
                    `<span class="project-branch">${escapeHtml(project.current_branch)}</span>` : '';
                const icon = project.is_git_repo ? '&#128193;' : '&#128194;';
                const taskCount = tasks.filter(t => t.project_id === project.id).length;
                const countHtml = taskCount > 0 ? `<span class="project-task-count">${taskCount}</span>` : '';

                // Git status badge
                const gitBadge = project.is_git_repo
                    ? '<span class="project-git-badge git">Git</span>'
                    : '<span class="project-git-badge no-git">No Git</span>';

                // Action buttons
                let actionsHtml = '<div class="project-actions">';
                if (!project.is_git_repo) {
                    actionsHtml += '<button class="btn btn-small btn-secondary btn-init-git" title="Initialize Git">Init</button>';
                } else {
                    actionsHtml += '<button class="btn btn-small btn-secondary btn-create-repo" title="Create GitHub Repo">+GH</button>';
                }
                actionsHtml += '</div>';

                $container.append(`
                    <div class="project-item" data-project-id="${project.id}">
                        <span class="project-icon">${icon}</span>
                        <span class="project-name" title="${escapeHtml(project.name)}">${escapeHtml(project.name)}</span>
                        ${gitBadge}
                        ${branchHtml}
                        ${countHtml}
                        ${actionsHtml}
                    </div>
                `);
            });
        }
    }

    // Count all projects in a folder and its subfolders
    function countProjectsInFolder(folder) {
        let count = folder.projects.length;
        Object.keys(folder.children).forEach(function(key) {
            count += countProjectsInFolder(folder.children[key]);
        });
        return count;
    }

    // Toggle folder collapse state (default is collapsed, false=expanded)
    function toggleFolder(folderPath) {
        // If currently collapsed (undefined or true), expand it (set to false)
        // If currently expanded (false), collapse it (delete to return to default)
        if (collapsedFolders[folderPath] === false) {
            delete collapsedFolders[folderPath]; // Return to default (collapsed)
        } else {
            collapsedFolders[folderPath] = false; // Expand
        }
        saveCollapsedState();
        renderProjectList();
    }

    function renderTaskTypeList() {
        const $list = $('.task-type-list');
        $list.empty();

        taskTypes.forEach(function(type) {
            const count = tasks.filter(t => t.task_type_id === type.id).length;
            const isSystem = type.is_system;
            $list.append(`
                <div class="task-type-item" data-type-id="${type.id}" data-is-system="${isSystem}">
                    <span class="task-type-color" style="background-color: ${type.color}"></span>
                    <span class="task-type-name">${escapeHtml(type.name)}</span>
                    <span class="task-type-count">${count}</span>
                    <span class="task-type-actions">
                        <button class="task-type-action-btn task-type-edit-btn" title="Bearbeiten">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                                <path d="M11.013 1.427a1.75 1.75 0 012.474 0l1.086 1.086a1.75 1.75 0 010 2.474l-8.61 8.61c-.21.21-.47.364-.756.445l-3.251.93a.75.75 0 01-.927-.928l.929-3.25a1.75 1.75 0 01.445-.758l8.61-8.61zm1.414 1.06a.25.25 0 00-.354 0L10.811 3.75l1.439 1.44 1.263-1.263a.25.25 0 000-.354l-1.086-1.086zM11.189 6.25L9.75 4.81l-6.286 6.287a.25.25 0 00-.064.108l-.558 1.953 1.953-.558a.249.249 0 00.108-.064l6.286-6.286z"/>
                            </svg>
                        </button>
                        <button class="task-type-action-btn task-type-delete-btn ${isSystem ? 'hidden' : ''}" title="Loeschen">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                                <path d="M6.5 1.75a.25.25 0 01.25-.25h2.5a.25.25 0 01.25.25V3h-3V1.75zm4.5 0V3h2.25a.75.75 0 010 1.5H2.75a.75.75 0 010-1.5H5V1.75C5 .784 5.784 0 6.75 0h2.5C10.216 0 11 .784 11 1.75zM4.496 6.675a.75.75 0 10-1.492.15l.66 6.6A1.75 1.75 0 005.405 15h5.19c.9 0 1.652-.681 1.741-1.576l.66-6.6a.75.75 0 00-1.492-.149l-.66 6.6a.25.25 0 01-.249.225h-5.19a.25.25 0 01-.249-.225l-.66-6.6z"/>
                            </svg>
                        </button>
                    </span>
                </div>
            `);
        });
    }

    function populateProjectSelect() {
        const $select = $('#taskProject');
        $select.find('option:not(:first)').remove();
        projects.forEach(function(project) {
            $select.append(`<option value="${project.id}">${escapeHtml(project.name)}</option>`);
        });
    }

    function populateTaskTypeSelect() {
        const $select = $('#taskType');
        $select.find('option:not(:first)').remove();
        taskTypes.forEach(function(type) {
            $select.append(`<option value="${type.id}" style="color: ${type.color}">${escapeHtml(type.name)}</option>`);
        });
    }

    function renderBranchRules() {
        const $list = $('#branchRulesList');
        $list.empty();

        if (branchRules.length === 0) {
            $list.html('<span style="color: var(--text-secondary); font-size: 0.8rem;">No rules defined</span>');
            return;
        }

        branchRules.forEach(function(rule) {
            $list.append(`
                <span class="branch-rule-tag" data-rule-id="${rule.id}">
                    ${escapeHtml(rule.branch_pattern)}
                    <button class="remove-rule" data-rule-id="${rule.id}">&times;</button>
                </span>
            `);
        });
    }

    function renderScanResults() {
        const $list = $('#scanResultsList');
        $list.empty();

        if (scannedRepos.length === 0) {
            $list.html('<div style="padding: 1rem; text-align: center; color: var(--text-secondary);">No repositories found</div>');
            return;
        }

        scannedRepos.forEach(function(repo) {
            $list.append(`
                <div class="scan-result-item">
                    <input type="checkbox" checked data-path="${escapeHtml(repo.path)}">
                    <span class="scan-result-path">${escapeHtml(repo.path)}</span>
                </div>
            `);
        });
    }

    // Event Listeners
    function setupEventListeners() {
        // ============================================================================
        // SIDEBAR CONTROLS
        // ============================================================================

        // Sidebar toggle button
        $('#sidebarToggle').on('click', function() {
            toggleSidebar();
        });

        // Click on selected project display in header opens sidebar
        // (but not when clicking on branch selector or push button)
        $('#selectedProjectDisplay').on('click', function(e) {
            // Don't open sidebar if clicking branch selector or push button
            if ($(e.target).closest('#branchSelector, #pushBtn').length) {
                return;
            }
            openSidebar();
        });

        // Sidebar overlay click (outside sidebar)
        $('#sidebarOverlay').on('click', function() {
            closeSidebar();
        });

        // Escape key closes sidebar
        $(document).on('keydown', function(e) {
            if (e.key === 'Escape' && sidebarOpen) {
                closeSidebar();
            }
        });

        // ============================================================================

        // Settings button (in user dropdown)
        $('#btnOpenSettings').on('click', function() {
            $('#userProfile').removeClass('open');
            openSettingsModal();
        });
        $('#btnSaveSettings').on('click', saveSettings);
        $('#btnValidateSettings').on('click', validateSettingsToken);
        $('.settings-close').on('click', closeSettingsModal);

        // Settings tabs
        $(document).on('click', '.settings-tab', function() {
            const tab = $(this).data('tab');
            $('.settings-tab').removeClass('active');
            $(this).addClass('active');
            $('.settings-content').removeClass('active');
            $('#settings-' + tab).addClass('active');
        });

        // Theme selector - live preview when changed
        $(document).on('change', 'input[name="themeChoice"]', function() {
            const newTheme = $(this).val();
            setThemePreference(newTheme);
        });

        // Token visibility toggle
        $('#btnToggleToken').on('click', function() {
            const $input = $('#settingsGithubToken');
            if ($input.attr('type') === 'password') {
                $input.attr('type', 'text');
                $(this).text('Hide');
            } else {
                $input.attr('type', 'password');
                $(this).text('Zeigen');
            }
        });

        // Browse button for settings
        $('#btnBrowseSettingsDir').on('click', function() {
            folderBrowserTarget = 'settings';
            openFolderBrowser();
        });

        // User profile dropdown
        $('#userProfileTrigger').on('click', function(e) {
            e.stopPropagation();
            $('#userProfile').toggleClass('open');
        });

        // Close dropdown when clicking outside
        $(document).on('click', function(e) {
            if (!$(e.target).closest('#userProfile').length) {
                $('#userProfile').removeClass('open');
            }
        });

        // User dropdown actions
        $('#btnConnectGithub').on('click', function() {
            $('#userProfile').removeClass('open');
            openSettingsModal();
            // Switch to GitHub tab
            $('.settings-tab').removeClass('active');
            $('.settings-tab[data-tab="github"]').addClass('active');
            $('.settings-content').removeClass('active');
            $('#settings-github').addClass('active');
        });

        $('#btnDisconnectGithub').on('click', function() {
            if (confirm('Disconnect from GitHub?')) {
                disconnectGithub();
            }
        });

        // Quick theme toggle in dropdown - cycles through dark -> light -> system
        $('#btnToggleTheme').on('click', function() {
            const currentTheme = getSavedTheme();
            let nextTheme;
            if (currentTheme === 'dark') {
                nextTheme = 'light';
            } else if (currentTheme === 'light') {
                nextTheme = 'system';
            } else {
                nextTheme = 'dark';
            }
            setThemePreference(nextTheme);
            // Update the label to show current mode
            const labels = { 'dark': 'Dark', 'light': 'Light', 'system': 'System' };
            $('.theme-toggle-label').text(labels[nextTheme]);
        });

        // Add task buttons
        $('.btn-add').on('click', function() {
            const status = $(this).data('status');
            openNewTaskModal(status);
        });

        // Task card clicks
        $(document).on('click', '.task-card', function(e) {
            // Don't open modal if clicking interactive elements
            if ($(e.target).hasClass('btn-live')) return;
            if ($(e.target).hasClass('btn-merge') || $(e.target).closest('.btn-merge').length) return;
            if ($(e.target).closest('.merge-conflict-section').length) return;
            if ($(e.target).closest('.task-card-actions').length) return;
            // Don't open modal when clicking branch link (let link open in new tab)
            if ($(e.target).closest('.branch-link').length) return;
            const taskId = $(this).data('id');
            const task = tasks.find(t => t.id === taskId);
            if (task) {
                openEditTaskModal(task);
            } else {
                console.warn('Task not found in tasks array:', taskId);
            }
        });

        // LIVE button click
        $(document).on('click', '.btn-live', function(e) {
            e.stopPropagation();
            const taskId = $(this).data('id');
            const task = tasks.find(t => t.id === taskId);
            if (task) {
                openEditTaskModal(task);
                setTimeout(function() {
                    const $log = $('#logOutput');
                    if ($log.length) {
                        $log[0].scrollIntoView({ behavior: 'smooth', block: 'center' });
                    }
                }, 100);
            }
        });

        // Merge button click (legacy, kept for conflict section)
        $(document).on('click', '.btn-merge', function(e) {
            e.stopPropagation();
            const taskId = $(this).data('id');
            mergeTaskToMain(taskId, $(this));
        });

        // Task dropdown toggle
        $(document).on('click', '.task-dropdown-toggle', function(e) {
            e.stopPropagation();
            const $toggle = $(this);
            const $menu = $toggle.siblings('.task-dropdown-menu');
            const isOpen = $menu.hasClass('open');

            // Close all other dropdowns first
            $('.task-dropdown-menu.open').removeClass('open');
            $('.task-dropdown-toggle.active').removeClass('active');

            if (!isOpen) {
                $toggle.addClass('active');
                $menu.addClass('open');
            }
        });

        // Task dropdown item click
        $(document).on('click', '.task-dropdown-item', function(e) {
            e.stopPropagation();
            const action = $(this).data('action');
            const taskId = $(this).data('id');

            // Close dropdown
            $(this).closest('.task-dropdown-menu').removeClass('open');
            $(this).closest('.task-card-actions').find('.task-dropdown-toggle').removeClass('active');

            if (action === 'merge') {
                mergeTaskToMain(taskId, $(this));
            }
        });

        // Close dropdown when clicking outside
        $(document).on('click', function(e) {
            if (!$(e.target).closest('.task-card-actions').length) {
                $('.task-dropdown-menu.open').removeClass('open');
                $('.task-dropdown-toggle.active').removeClass('active');
            }
            // Close branch dropdown
            if (!$(e.target).closest('.branch-selector').length) {
                closeBranchDropdown();
            }
        });

        // Branch selector events
        $('#branchSelectorBtn').on('click', function(e) {
            e.stopPropagation();
            toggleBranchDropdown();
        });

        $(document).on('click', '.branch-dropdown-item', function(e) {
            e.stopPropagation();
            const action = $(this).data('action');

            // Handle "Create new branch" action
            if (action === 'create-branch') {
                const branchName = prompt('Enter new branch name (will be created from main):');
                if (branchName && branchName.trim()) {
                    const sanitized = branchName.trim().replace(/[^a-zA-Z0-9\-_\/]/g, '-');
                    if (selectedProjectFilter) {
                        setWorkingBranch(selectedProjectFilter, sanitized, true);
                    }
                }
                return;
            }

            // Handle branch switch
            const branch = $(this).data('branch');
            if (branch && selectedProjectFilter && !$(this).hasClass('active')) {
                switchToBranch(selectedProjectFilter, branch);
            }
        });

        $('#branchPullBtn').on('click', function(e) {
            e.stopPropagation();
            if (selectedProjectFilter) pullBranch(selectedProjectFilter);
        });

        // Push button click (Trunk-based development)
        $('#pushBtn').on('click', function(e) {
            e.stopPropagation();
            if (selectedProjectFilter) pushToRemote(selectedProjectFilter);
        });

        // Rollback button click on task cards (Trunk-based development)
        $(document).on('click', '.btn-rollback', function(e) {
            e.stopPropagation();
            const taskId = $(this).closest('.task-card').data('id');
            rollbackTask(taskId);
        });

        // Project click in sidebar - select project and close sidebar
        $(document).on('click', '.project-item', function(e) {
            // Don't select project if clicking action buttons
            if ($(e.target).closest('.project-actions').length) {
                return;
            }
            const projectId = $(this).data('project-id');
            selectProject(projectId, true); // true = close sidebar after selection
        });

        // Double-click to edit project
        $(document).on('dblclick', '.project-item[data-project-id!=""]', function(e) {
            const projectId = $(this).data('project-id');
            const project = projects.find(p => p.id === projectId);
            if (project) openEditProjectModal(project);
        });

        // Folder toggle click
        $(document).on('click', '.project-folder-header', function(e) {
            const folderPath = $(this).closest('.project-folder').data('folder-path');
            toggleFolder(folderPath);
        });

        // Task type edit button click
        $(document).on('click', '.task-type-edit-btn', function(e) {
            e.stopPropagation();
            const typeId = $(this).closest('.task-type-item').data('type-id');
            const type = taskTypes.find(t => t.id === typeId);
            if (type) openEditTaskTypeModal(type);
        });

        // Task type delete button click
        $(document).on('click', '.task-type-delete-btn', function(e) {
            e.stopPropagation();
            const $item = $(this).closest('.task-type-item');
            const typeId = $item.data('type-id');
            const type = taskTypes.find(t => t.id === typeId);
            if (type && !type.is_system) {
                showDeleteTaskTypeConfirmation(type);
            }
        });

        // Task type double-click in sidebar (legacy support)
        $(document).on('dblclick', '.task-type-item', function(e) {
            const typeId = $(this).data('type-id');
            const type = taskTypes.find(t => t.id === typeId);
            if (type) openEditTaskTypeModal(type);
        });

        // Add project button
        $('#btnAddProject').on('click', function() {
            openNewProjectModal();
        });

        // Refresh projects button
        $('#btnRefreshProjects').on('click', function() {
            loadProjects();
            showToast('Projects refreshed', 'success');
        });

        // Scan projects button
        $('#btnScanProjects').on('click', function() {
            openScanModal();
        });

        // Add task type button
        $('#btnAddTaskType').on('click', function() {
            openNewTaskTypeModal();
        });

        // Modal close buttons
        $('.close-btn').on('click', function() {
            closeModal();
            closeProjectModal();
            closeScanModal();
            closeTaskTypeModal();
            closeSettingsModal();
            closeGithubModal();
            closeCreateRepoModal();
            closeDeployModal();
        });

        $('.project-close').on('click', closeProjectModal);
        $('.scan-close').on('click', closeScanModal);
        $('.tasktype-close').on('click', closeTaskTypeModal);
        $('.github-close').on('click', closeGithubModal);
        $('.repo-close').on('click', closeCreateRepoModal);
        $('.deploy-close').on('click', closeDeployModal);

        $('#taskModal').on('click', function(e) {
            if (e.target === this) closeModal();
        });
        $('#projectModal').on('click', function(e) {
            if (e.target === this) closeProjectModal();
        });
        $('#scanModal').on('click', function(e) {
            if (e.target === this) closeScanModal();
        });
        $('#taskTypeModal').on('click', function(e) {
            if (e.target === this) closeTaskTypeModal();
        });
        $('#settingsModal').on('click', function(e) {
            if (e.target === this) closeSettingsModal();
        });
        $('#githubModal').on('click', function(e) {
            if (e.target === this) closeGithubModal();
        });
        $('#createRepoModal').on('click', function(e) {
            if (e.target === this) closeCreateRepoModal();
        });
        $('#deployModal').on('click', function(e) {
            if (e.target === this) closeDeployModal();
        });

        $(document).on('keydown', function(e) {
            if (e.key === 'Escape') {
                closeModal();
                closeProjectModal();
                closeScanModal();
                closeTaskTypeModal();
                closeSettingsModal();
                closeGithubModal();
                closeCreateRepoModal();
                closeDeployModal();
            }
        });

        // Task form submit
        $('#taskForm').on('submit', function(e) {
            e.preventDefault();
            submitTaskForm();
        });

        $('#btnSave').on('click', function(e) {
            e.preventDefault();
            submitTaskForm();
        });

        $('#btnDelete').on('click', function() {
            if (confirm('Really delete this task?')) {
                deleteTask(currentTaskId);
            }
        });

        // Project change updates project_dir
        $('#taskProject').on('change', function() {
            const projectId = $(this).val();
            if (projectId) {
                const project = projects.find(p => p.id === projectId);
                if (project) {
                    $('#taskProjectDir').val(project.path);
                    $('#projectDirGroup').addClass('hidden');
                }
            } else {
                $('#projectDirGroup').removeClass('hidden');
            }
        });

        // Project form
        $('#btnSaveProject').on('click', function() {
            submitProjectForm();
        });

        $('#btnDeleteProject').on('click', function() {
            if (confirm('Really delete this project? All linked tasks will lose their project association.')) {
                deleteProject(currentProjectId);
            }
        });

        // Branch rules
        $('#btnAddBranchRule').on('click', function() {
            const pattern = $('#newBranchRule').val().trim();
            if (pattern && currentProjectId) {
                addBranchRule(currentProjectId, pattern);
            }
        });

        $(document).on('click', '.remove-rule', function() {
            const ruleId = $(this).data('rule-id');
            deleteBranchRule(ruleId);
        });

        // Scan form
        $('#btnStartScan').on('click', function() {
            const basePath = $('#scanBasePath').val().trim();
            const maxDepth = parseInt($('#scanDepth').val());
            if (basePath) {
                scanProjects(basePath, maxDepth);
            } else {
                showToast('Please specify base directory', 'error');
            }
        });

        $('#btnImportScan').on('click', function() {
            importScannedProjects();
        });

        $('#btnCancelScan').on('click', closeScanModal);

        // Task type form
        $('#btnSaveTaskType').on('click', function() {
            submitTaskTypeForm();
        });

        $('#btnDeleteTaskType').on('click', function() {
            if (confirm('Really delete this task type?')) {
                deleteTaskType(currentTaskTypeId);
            }
        });

        // Color picker preview
        $('#taskTypeColor').on('input', function() {
            $('#taskTypeColorPreview').css('background-color', $(this).val());
        });

        // RALPH controls
        $('#btnPause').on('click', function() {
            pauseTask(currentTaskId);
        });

        $('#btnResume').on('click', function() {
            resumeTask(currentTaskId);
        });

        $('#btnStop').on('click', function() {
            if (confirm('Really stop this process?')) {
                stopTask(currentTaskId);
            }
        });

        $('#btnFeedback').on('click', function() {
            const message = $('#feedbackInput').val().trim();
            if (message) {
                sendFeedback(currentTaskId, message);
            }
        });

        $('#feedbackInput').on('keypress', function(e) {
            if (e.key === 'Enter') {
                $('#btnFeedback').click();
            }
        });

        // Continue Task button (for review/blocked tasks)
        $('#btnContinueTask').on('click', function() {
            const message = $('#continueTaskInput').val().trim();
            continueTaskWithMessage(currentTaskId, message);
        });

        // Allow Ctrl+Enter to submit continue task
        $('#continueTaskInput').on('keypress', function(e) {
            if (e.key === 'Enter' && e.ctrlKey) {
                $('#btnContinueTask').click();
            }
        });

        $('#autoScroll').on('change', function() {
            autoScroll = $(this).is(':checked');
            // When re-enabled, immediately scroll to bottom
            if (autoScroll) {
                const $log = $('#logOutput');
                scrollToBottom($log);
            }
        });

        $('#btnReconnect').on('click', function() {
            connectWebSocket();
        });

        // Folder browser
        $('#btnBrowse').on('click', function() {
            folderBrowserTarget = 'task';
            openFolderBrowser();
        });

        $('#btnBrowseProject').on('click', function() {
            folderBrowserTarget = 'project';
            openFolderBrowser();
        });

        $('#btnBrowseScan').on('click', function() {
            folderBrowserTarget = 'scan';
            openFolderBrowser();
        });

        $('#btnParentDir').on('click', function() {
            if (folderBrowserPath) {
                loadFolder($('#currentPath').data('parent') || '');
            }
        });

        $(document).on('click', '.folder-item', function() {
            const path = $(this).data('path');
            if ($(this).hasClass('selected')) {
                loadFolder(path);
            } else {
                $('.folder-item').removeClass('selected');
                $(this).addClass('selected');
                selectedFolderPath = path;
            }
        });

        $(document).on('dblclick', '.folder-item', function() {
            const path = $(this).data('path');
            loadFolder(path);
        });

        $('#btnCreateFolder').on('click', function() {
            const name = $('#newFolderName').val().trim();
            if (name) {
                createFolder(folderBrowserPath + '/' + name);
            }
        });

        $('#btnSelectFolder').on('click', function() {
            const path = selectedFolderPath || folderBrowserPath;
            if (folderBrowserTarget === 'task') {
                $('#taskProjectDir').val(path);
            } else if (folderBrowserTarget === 'project') {
                $('#projectPath').val(path);
                // Auto-fill project name from folder name
                if (!$('#projectName').val()) {
                    $('#projectName').val(path.split('/').pop());
                }
            } else if (folderBrowserTarget === 'scan') {
                $('#scanBasePath').val(path);
            } else if (folderBrowserTarget === 'settings') {
                $('#settingsProjectDir').val(path);
            }
            closeFolderModal();
        });

        $('#btnCancelFolder').on('click', closeFolderModal);
        $('.folder-close').on('click', closeFolderModal);

        $('#folderModal').on('click', function(e) {
            if (e.target === this) closeFolderModal();
        });

        // GitHub Settings
        $('#btnGitHubSettings').on('click', openGithubModal);
        $('#btnValidateGithub').on('click', validateGithubToken);
        $('#btnSaveGithub').on('click', function() {
            const token = $('#githubToken').val().trim();
            if (token) {
                saveGithubToken(token);
            }
        });

        // Create Repo Modal
        $('#btnCancelRepo').on('click', closeCreateRepoModal);
        $('#btnCreateRepo').on('click', function() {
            const projectId = $('#createRepoProjectId').val();
            const repoName = $('#repoName').val().trim();
            const description = $('#repoDescription').val().trim();
            const isPrivate = $('#repoPrivate').is(':checked');
            if (repoName) {
                createGithubRepo(projectId, repoName, description, isPrivate);
            }
        });

        // Deploy Modal
        $('#btnCancelDeploy').on('click', closeDeployModal);
        $('#btnConfirmDeploy').on('click', function() {
            const taskId = $('#deployTaskId').val();
            const message = $('#deployCommitMessage').val().trim();
            deployTask(taskId, message);
        });

        // Project action buttons (delegated)
        $(document).on('click', '.btn-init-git', function(e) {
            e.stopPropagation();
            const projectId = $(this).closest('.project-item').data('project-id');
            if (confirm('Initialize Git repository?')) {
                initializeGit(projectId);
            }
        });

        $(document).on('click', '.btn-create-repo', function(e) {
            e.stopPropagation();
            const projectId = $(this).closest('.project-item').data('project-id');
            openCreateRepoModal(projectId);
        });
    }

    // Drag and Drop
    function setupDragAndDrop() {
        $(document).on('dragstart', '.task-card', function(e) {
            $(this).addClass('dragging');
            e.originalEvent.dataTransfer.setData('text/plain', $(this).data('id'));
            e.originalEvent.dataTransfer.effectAllowed = 'move';
        });

        $(document).on('dragend', '.task-card', function() {
            $(this).removeClass('dragging');
        });

        $('.tasks-container').on('dragover', function(e) {
            e.preventDefault();
            e.originalEvent.dataTransfer.dropEffect = 'move';
            $(this).addClass('drag-over');
        });

        $('.tasks-container').on('dragleave', function() {
            $(this).removeClass('drag-over');
        });

        $('.tasks-container').on('drop', function(e) {
            e.preventDefault();
            $(this).removeClass('drag-over');

            const taskId = e.originalEvent.dataTransfer.getData('text/plain');
            const newStatus = $(this).closest('.column').data('status');
            const task = tasks.find(t => t.id === taskId);

            if (task && task.status !== newStatus) {
                task.status = newStatus;
                renderAllTasks();
                updateTaskStatus(taskId, newStatus);

                // On mobile, switch to the target column
                if (isMobileView()) {
                    switchMobileTab(newStatus);
                }
            }
        });

        let touchStartX, touchStartY, draggedElement;

        $(document).on('touchstart', '.task-card', function(e) {
            const touch = e.originalEvent.touches[0];
            touchStartX = touch.clientX;
            touchStartY = touch.clientY;
            draggedElement = this;

            $(this).data('touchTimer', setTimeout(function() {
                $(draggedElement).addClass('dragging');
            }, 500));
        });

        $(document).on('touchmove', '.task-card', function(e) {
            clearTimeout($(this).data('touchTimer'));
        });

        $(document).on('touchend', '.task-card', function(e) {
            clearTimeout($(this).data('touchTimer'));
            $(this).removeClass('dragging');
        });

    }

    // Sidebar Resize Functionality
    function setupSidebarResize() {
        const sidebar = document.querySelector('.sidebar');
        const resizeHandle = document.getElementById('sidebarResizeHandle');

        if (!sidebar || !resizeHandle) return;

        const DEFAULT_WIDTH = 280;
        const MIN_WIDTH = 200;
        const MAX_WIDTH = 500;
        const STORAGE_KEY = 'runner-sidebar-width';

        let isResizing = false;
        let startX, startWidth;

        // Helper to update sidebar width and CSS variable
        function setSidebarWidth(width) {
            const clampedWidth = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, width));
            sidebar.style.width = clampedWidth + 'px';
            document.documentElement.style.setProperty('--sidebar-width', clampedWidth + 'px');
            resizeHandle.style.left = (clampedWidth - 3) + 'px';
            return clampedWidth;
        }

        // Load saved width from localStorage
        const savedWidth = localStorage.getItem(STORAGE_KEY);
        if (savedWidth) {
            const width = parseInt(savedWidth);
            if (width >= MIN_WIDTH && width <= MAX_WIDTH) {
                setSidebarWidth(width);
            }
        } else {
            // Set default width
            setSidebarWidth(DEFAULT_WIDTH);
        }

        // Start resize (mouse or touch)
        function startResize(clientX) {
            isResizing = true;
            startX = clientX;
            startWidth = sidebar.offsetWidth;

            resizeHandle.classList.add('dragging');
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
        }

        // During resize (mouse or touch)
        function doResize(clientX) {
            if (!isResizing) return;

            const diff = clientX - startX;
            const newWidth = startWidth + diff;
            setSidebarWidth(newWidth);
        }

        // End resize (mouse or touch)
        function endResize() {
            if (!isResizing) return;

            isResizing = false;
            resizeHandle.classList.remove('dragging');
            document.body.style.cursor = '';
            document.body.style.userSelect = '';

            // Save width to localStorage
            localStorage.setItem(STORAGE_KEY, sidebar.offsetWidth);
        }

        // Mouse events
        resizeHandle.addEventListener('mousedown', function(e) {
            startResize(e.clientX);
            e.preventDefault();
        });

        document.addEventListener('mousemove', function(e) {
            doResize(e.clientX);
        });

        document.addEventListener('mouseup', function() {
            endResize();
        });

        // Touch events for tablet support
        resizeHandle.addEventListener('touchstart', function(e) {
            if (e.touches.length === 1) {
                startResize(e.touches[0].clientX);
                e.preventDefault();
            }
        }, { passive: false });

        document.addEventListener('touchmove', function(e) {
            if (isResizing && e.touches.length === 1) {
                doResize(e.touches[0].clientX);
                e.preventDefault();
            }
        }, { passive: false });

        document.addEventListener('touchend', function() {
            endResize();
        });

        document.addEventListener('touchcancel', function() {
            endResize();
        });

        // Double-click to reset to default width
        resizeHandle.addEventListener('dblclick', function() {
            setSidebarWidth(DEFAULT_WIDTH);
            localStorage.setItem(STORAGE_KEY, DEFAULT_WIDTH);
        });
    }

    // Task Modal Functions
    function openNewTaskModal(status) {
        currentTaskId = null;
        $('#modalTitle').text('New Task');
        $('#taskId').val('');
        $('#taskTitle').val('');
        $('#taskDescription').val('');
        $('#taskCriteria').val('');
        $('#taskProject').val(selectedProjectFilter || '');
        $('#taskType').val('');
        $('#taskPriority').val('2');
        $('#taskMaxIterations').val(config.default_max_iterations || 10);
        $('#taskProjectDir').val('');

        // Show/hide project dir based on project selection
        if (selectedProjectFilter) {
            const project = projects.find(p => p.id === selectedProjectFilter);
            if (project) {
                $('#taskProjectDir').val(project.path);
                $('#projectDirGroup').addClass('hidden');
            }
        } else {
            $('#projectDirGroup').removeClass('hidden');
        }

        $('#btnDelete').addClass('hidden');
        $('#ralphControls').addClass('hidden');
        $('#logSection').addClass('hidden');
        $('#errorSection').addClass('hidden');
        $('#branchInfoGroup').addClass('hidden');

        // Clear attachments for new task
        clearAttachmentList();

        $('#taskModal').addClass('active');
        $('#taskTitle').focus();
    }

    function openEditTaskModal(task) {
        currentTaskId = task.id;
        $('#modalTitle').text('Edit Task');
        $('#taskId').val(task.id);
        $('#taskTitle').val(task.title);
        $('#taskDescription').val(task.description || '');
        $('#taskCriteria').val(task.acceptance_criteria || '');
        $('#taskProject').val(task.project_id || '');
        $('#taskType').val(task.task_type_id || '');
        $('#taskPriority').val(task.priority);
        $('#taskMaxIterations').val(task.max_iterations);
        $('#taskProjectDir').val(task.project_dir || '');

        // Show/hide project dir based on project selection
        if (task.project_id) {
            $('#projectDirGroup').addClass('hidden');
        } else {
            $('#projectDirGroup').removeClass('hidden');
        }

        // Branch info
        if (task.working_branch) {
            $('#taskBranch').text(task.working_branch);
            $('#branchInfoGroup').removeClass('hidden');
        } else {
            $('#branchInfoGroup').addClass('hidden');
        }

        // Load attachments
        loadAttachments(task.id);

        $('#btnDelete').removeClass('hidden');

        if (task.status === 'progress') {
            $.get('/api/tasks/' + task.id)
                .done(function(freshTask) {
                    updateModalForTask(freshTask);
                })
                .fail(function() {
                    updateModalForTask(task);
                });
        } else {
            updateModalForTask(task);
        }

        $('#taskModal').addClass('active');
    }

    function updateModalForTask(task) {
        // RALPH controls (pause/resume/stop) - only for running tasks
        if (task.status === 'progress') {
            $('#ralphControls').removeClass('hidden');
            $('#btnPause').removeClass('hidden');
            $('#btnResume').addClass('hidden');
        } else {
            $('#ralphControls').addClass('hidden');
        }

        // Feedback section - show only for running tasks (progress status)
        if (task.status === 'progress') {
            $('#feedbackSection').removeClass('hidden');
            $('#feedbackLabel').text('Feedback to Claude:');
            $('#feedbackHelp').text('Send feedback to running process');
            $('#btnFeedback').text('Send');
        } else {
            $('#feedbackSection').addClass('hidden');
        }

        // Continue Task section - show for review/blocked tasks (below logs)
        if (task.status === 'review' || task.status === 'blocked') {
            $('#continueTaskSection').removeClass('hidden');
            $('#continueTaskInput').val(''); // Clear previous input
        } else {
            $('#continueTaskSection').addClass('hidden');
        }

        // Log section
        if (task.status === 'progress' || task.logs) {
            const wasHidden = $('#logSection').hasClass('hidden');
            $('#logSection').removeClass('hidden');

            if (task.status === 'progress' && !task.logs) {
                resetLogState();
                $('#logOutput').html('<span class="waiting">Claude is starting... waiting for output...</span>');
            } else {
                // Parse existing logs with the new structured system
                parseExistingLogs(task.logs || '');
            }

            const badgeText = task.current_iteration > 0
                ? 'Iteration ' + task.current_iteration
                : 'Running...';
            $('#iterationBadge').text(badgeText);

            // Only initialize scroll detection once when log section first becomes visible
            if (wasHidden) {
                setupLogScrollDetection();
                setupLogFilters();
                startTimestampUpdates(); // Start updating timestamps
                // Reset auto-scroll state when first opening
                autoScroll = true;
                $('#autoScroll').prop('checked', true);
                // Reset filter state
                logState.filter = 'all';
                logState.searchQuery = '';
                $('.filter-btn').removeClass('active');
                $('.filter-btn[data-filter="all"]').addClass('active');
                $('#logSearch').val('');
            }

            if (autoScroll) {
                const $log = $('#logOutput');
                scrollToBottom($log);
            }
        } else {
            $('#logSection').addClass('hidden');
        }

        // Error section
        if (task.status === 'blocked' && task.error) {
            $('#errorSection').removeClass('hidden');
            $('#errorMessage').text(task.error);
        } else {
            $('#errorSection').addClass('hidden');
        }
    }

    function closeModal() {
        $('#taskModal').removeClass('active');
        currentTaskId = null;
        clearPendingAttachments(); // Clear any pending attachments when modal closes
        stopTimestampUpdates(); // Stop updating timestamps when modal closes
    }

    function submitTaskForm() {
        const projectId = $('#taskProject').val();
        let projectDir = $('#taskProjectDir').val().trim();

        // If project selected, use project's path
        if (projectId) {
            const project = projects.find(p => p.id === projectId);
            if (project) projectDir = project.path;
        }

        const taskData = {
            title: $('#taskTitle').val().trim(),
            description: $('#taskDescription').val(),
            acceptance_criteria: $('#taskCriteria').val(),
            project_id: projectId || '',
            task_type_id: $('#taskType').val() || '',
            priority: parseInt($('#taskPriority').val()),
            max_iterations: parseInt($('#taskMaxIterations').val()),
            project_dir: projectDir
        };

        if (!taskData.title) {
            showToast('Title is required', 'error');
            return;
        }

        const taskId = $('#taskId').val();
        if (taskId) {
            taskData.id = taskId;
        }

        saveTask(taskData);
    }

    // Project Modal Functions
    function openNewProjectModal() {
        currentProjectId = null;
        branchRules = [];
        $('#projectModalTitle').text('New Project');
        $('#projectId').val('');
        $('#projectName').val('');
        $('#projectPath').val('');
        $('#projectDescription').val('');
        renderBranchRules();
        $('#btnDeleteProject').addClass('hidden');
        $('#projectModal').addClass('active');
    }

    function openEditProjectModal(project) {
        currentProjectId = project.id;
        $('#projectModalTitle').text('Edit Project');
        $('#projectId').val(project.id);
        $('#projectName').val(project.name);
        $('#projectPath').val(project.path);
        $('#projectDescription').val(project.description || '');
        loadBranchRules(project.id);
        $('#btnDeleteProject').removeClass('hidden');
        $('#projectModal').addClass('active');
    }

    function closeProjectModal() {
        $('#projectModal').removeClass('active');
        currentProjectId = null;
        branchRules = [];
    }

    function submitProjectForm() {
        const projectData = {
            name: $('#projectName').val().trim(),
            path: $('#projectPath').val().trim(),
            description: $('#projectDescription').val()
        };

        if (!projectData.name || !projectData.path) {
            showToast('Name and path are required', 'error');
            return;
        }

        if (currentProjectId) {
            projectData.id = currentProjectId;
        }

        saveProject(projectData);
    }

    // Scan Modal Functions
    function openScanModal() {
        scannedRepos = [];
        $('#scanBasePath').val(config.default_project_dir || '');
        $('#scanDepth').val('4');
        $('#scanResults').addClass('hidden');
        $('#scanResultsList').empty();
        $('#btnStartScan').removeClass('hidden');
        $('#btnImportScan').addClass('hidden');
        $('#scanModal').addClass('active');
    }

    function closeScanModal() {
        $('#scanModal').removeClass('active');
        scannedRepos = [];
    }

    // Task Type Modal Functions
    function openNewTaskTypeModal() {
        currentTaskTypeId = null;
        $('#taskTypeModalTitle').text('New Task Type');
        $('#taskTypeId').val('');
        $('#taskTypeName').val('');
        $('#taskTypeColor').val('#58a6ff');
        $('#taskTypeColorPreview').css('background-color', '#58a6ff');
        $('#btnDeleteTaskType').addClass('hidden');
        $('#taskTypeModal').addClass('active');
    }

    function openEditTaskTypeModal(type) {
        currentTaskTypeId = type.id;
        $('#taskTypeModalTitle').text('Edit Task Type');
        $('#taskTypeId').val(type.id);
        $('#taskTypeName').val(type.name);
        $('#taskTypeColor').val(type.color);
        $('#taskTypeColorPreview').css('background-color', type.color);

        // Can't delete system types
        if (type.is_system) {
            $('#btnDeleteTaskType').addClass('hidden');
        } else {
            $('#btnDeleteTaskType').removeClass('hidden');
        }

        $('#taskTypeModal').addClass('active');
    }

    function closeTaskTypeModal() {
        $('#taskTypeModal').removeClass('active');
        currentTaskTypeId = null;
    }

    function submitTaskTypeForm() {
        const typeData = {
            name: $('#taskTypeName').val().trim(),
            color: $('#taskTypeColor').val()
        };

        if (!typeData.name) {
            showToast('Name is required', 'error');
            return;
        }

        if (currentTaskTypeId) {
            typeData.id = currentTaskTypeId;
        }

        saveTaskType(typeData);
    }

    function showDeleteTaskTypeConfirmation(type) {
        const taskCount = tasks.filter(t => t.task_type_id === type.id).length;
        let message = `Really delete task type "${type.name}"?`;
        if (taskCount > 0) {
            message += `\n\n${taskCount} task(s) with this type will become untyped.`;
        }

        if (confirm(message)) {
            deleteTaskTypeWithReload(type.id);
        }
    }

    function deleteTaskTypeWithReload(typeId) {
        $.ajax({
            url: '/api/task-types/' + typeId,
            method: 'DELETE'
        })
        .done(function() {
            taskTypes = taskTypes.filter(t => t.id !== typeId);
            // Update tasks that had this type to have no type
            tasks.forEach(function(task) {
                if (task.task_type_id === typeId) {
                    task.task_type_id = '';
                    task.task_type = null;
                }
            });
            renderTaskTypeList();
            populateTaskTypeSelect();
            renderAllTasks();
            showToast('Task type deleted', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error deleting';
            showToast(msg, 'error');
        });
    }

    // Utility Functions
    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    function showToast(message, type) {
        const $toast = $(`<div class="toast ${type || ''}">${escapeHtml(message)}</div>`);
        $('#toastContainer').append($toast);

        setTimeout(function() {
            $toast.fadeOut(300, function() {
                $(this).remove();
            });
        }, 3000);
    }

    // Folder Browser Functions
    function openFolderBrowser() {
        selectedFolderPath = '';
        let startPath = '';

        if (folderBrowserTarget === 'task') {
            startPath = $('#taskProjectDir').val() || config.default_project_dir || '';
        } else if (folderBrowserTarget === 'project') {
            startPath = $('#projectPath').val() || config.default_project_dir || '';
        } else if (folderBrowserTarget === 'scan') {
            startPath = $('#scanBasePath').val() || config.default_project_dir || '';
        } else if (folderBrowserTarget === 'settings') {
            startPath = $('#settingsProjectDir').val() || config.default_project_dir || '';
        }

        loadFolder(startPath);
        $('#folderModal').addClass('active');
    }

    function closeFolderModal() {
        $('#folderModal').removeClass('active');
        selectedFolderPath = '';
    }

    function loadFolder(path) {
        $.get('/api/browse', { path: path })
            .done(function(data) {
                folderBrowserPath = data.current_path;
                selectedFolderPath = data.current_path;
                $('#currentPath').val(data.current_path);
                $('#currentPath').data('parent', data.parent_path);
                renderFolderList(data.directories, data.is_repo);
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Error loading';
                showToast(msg, 'error');
            });
    }

    function renderFolderList(directories, currentIsRepo) {
        const $list = $('#folderList');
        $list.empty();

        if (!directories || directories.length === 0) {
            $list.html('<div class="folder-empty">No subfolders</div>');
            return;
        }

        directories.forEach(function(dir) {
            const icon = dir.is_repo ? '&#128193;' : '&#128194;';
            const badge = dir.is_repo ? '<span class="folder-badge">Git</span>' : '';
            $list.append(`
                <div class="folder-item" data-path="${escapeHtml(dir.path)}">
                    <span class="folder-icon">${icon}</span>
                    <span class="folder-name">${escapeHtml(dir.name)}${badge}</span>
                </div>
            `);
        });
    }

    function createFolder(path) {
        $.ajax({
            url: '/api/browse/create',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ path: path })
        })
        .done(function(data) {
            $('#newFolderName').val('');
            showToast('Folder created', 'success');
            loadFolder(folderBrowserPath);
            selectedFolderPath = data.path;
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error creating';
            showToast(msg, 'error');
        });
    }

    // ============================================================================
    // GitHub Integration Functions
    // ============================================================================

    function saveGithubToken(token) {
        $.ajax({
            url: '/api/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify({ github_token: token })
        })
        .done(function(data) {
            config = data;
            showToast('GitHub token saved', 'success');
            closeGithubModal();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error saving';
            showToast(msg, 'error');
        });
    }

    function validateGithubToken() {
        const token = $('#githubToken').val().trim();
        if (!token) {
            showToast('Please enter token', 'error');
            return;
        }

        // Temporarily save and validate
        $.ajax({
            url: '/api/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify({ github_token: token })
        })
        .done(function() {
            $.post('/api/github/validate')
                .done(function(data) {
                    $('#githubStatus')
                        .removeClass('hidden error')
                        .addClass('success')
                        .html('<span class="github-status-icon">&#10003;</span>' +
                              '<span>Connected as <strong>' + escapeHtml(data.username) + '</strong></span>');
                })
                .fail(function(xhr) {
                    $('#githubStatus')
                        .removeClass('hidden success')
                        .addClass('error')
                        .html('<span class="github-status-icon">&#10060;</span>' +
                              '<span>Token invalid</span>');
                });
        });
    }

    function initializeGit(projectId) {
        $.post('/api/projects/' + projectId + '/git-init')
            .done(function(project) {
                const idx = projects.findIndex(p => p.id === project.id);
                if (idx !== -1) projects[idx] = project;
                renderProjectList();
                showToast('Git repository initialized', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Error initializing Git';
                showToast(msg, 'error');
            });
    }

    function createGithubRepo(projectId, repoName, description, isPrivate) {
        $('#btnCreateRepo').prop('disabled', true).text('Creating...');

        $.ajax({
            url: '/api/projects/' + projectId + '/github-repo',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({
                repo_name: repoName,
                description: description,
                private: isPrivate
            })
        })
        .done(function(data) {
            showToast('GitHub repository created: ' + data.repo_url, 'success');
            closeCreateRepoModal();
            loadProjects();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Error creating';
            showToast(msg, 'error');
        })
        .always(function() {
            $('#btnCreateRepo').prop('disabled', false).text('Create Repository');
        });
    }

    function deployTask(taskId, commitMessage) {
        $('#btnConfirmDeploy').prop('disabled', true);
        $('#deployStatus').removeClass('hidden');

        $.ajax({
            url: '/api/tasks/' + taskId + '/deploy',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ commit_message: commitMessage })
        })
        .done(function(data) {
            const msg = 'Deployment successful!' + (data.commit_hash ? ' Commit: ' + data.commit_hash.substring(0, 7) : '');
            showToast(msg, 'success');
            closeDeployModal();
            loadTasks();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Deployment failed';
            showToast(msg, 'error');
            $('#deployStatus').addClass('hidden');
        })
        .always(function() {
            $('#btnConfirmDeploy').prop('disabled', false);
        });
    }

    // ============================================================================
    // Merge to Main Functions
    // ============================================================================

    function mergeTaskToMain(taskId, $element) {
        const task = tasks.find(t => t.id === taskId);
        if (!task) return;

        // Show toast to indicate action started
        showToast('Merging branch...');

        // If element is a button with visible state, update it
        const isButton = $element && $element.hasClass('btn-merge');
        let originalText = '';
        if (isButton) {
            originalText = $element.html();
            $element.html('<span class="spinner-small"></span> Merging...');
            $element.prop('disabled', true);
            $element.addClass('merging');
        }

        $.ajax({
            url: '/api/tasks/' + taskId + '/merge',
            type: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({})
        })
        .done(function(data) {
            if (data.success) {
                // Success!
                if (isButton) {
                    $element.html('&#10003; Merged');
                    $element.removeClass('merging').addClass('merged');
                }
                showToast('Merged successfully!', 'success');

                // Reload tasks to update UI (branch badge gone, merge option gone)
                setTimeout(function() {
                    loadTasks();
                }, 1000);
            } else if (data.conflict && data.pr_url) {
                // Conflict - PR was created
                showToast('Conflict detected - PR created', 'warning');

                // Update task in local state
                const idx = tasks.findIndex(t => t.id === taskId);
                if (idx !== -1) {
                    tasks[idx].conflict_pr_url = data.pr_url;
                    tasks[idx].conflict_pr_number = data.pr_number;
                }

                // Re-render to show conflict state
                renderAllTasks();
            }
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Merge failed';
            showToast(msg, 'error');

            // Restore button if applicable
            if (isButton) {
                $element.html(originalText);
                $element.prop('disabled', false);
                $element.removeClass('merging');
            }
        });
    }

    // GitHub Modal Functions
    function openGithubModal() {
        $('#githubToken').val(config.github_token || '');
        $('#githubStatus').addClass('hidden');
        $('#githubModal').addClass('active');
    }

    function closeGithubModal() {
        $('#githubModal').removeClass('active');
    }

    function openCreateRepoModal(projectId) {
        const project = projects.find(p => p.id === projectId);
        if (!project) return;

        $('#createRepoProjectId').val(projectId);
        $('#repoName').val(project.name.toLowerCase().replace(/\s+/g, '-'));
        $('#repoDescription').val(project.description || '');
        $('#repoPrivate').prop('checked', false);
        $('#createRepoModal').addClass('active');
    }

    function closeCreateRepoModal() {
        $('#createRepoModal').removeClass('active');
    }

    function openDeployModal(taskId) {
        const task = tasks.find(t => t.id === taskId);
        if (!task) return;

        $('#deployTaskId').val(taskId);
        $('#deployTaskTitle').text(task.title);
        $('#deployCommitMessage').val('Deploy: ' + task.title);
        $('#deployStatus').addClass('hidden');
        $('#deployModal').addClass('active');
    }

    function closeDeployModal() {
        $('#deployModal').removeClass('active');
    }

    // ============================================================================
    // Settings Modal Functions
    // ============================================================================

    function openSettingsModal() {
        // Populate form fields from config
        $('#settingsProjectDir').val(config.default_project_dir || '');
        $('#settingsClaudeCommand').val(config.claude_command || 'claude');
        $('#settingsMaxIterations').val(config.default_max_iterations || 10);
        $('#settingsGithubToken').val(config.github_token || '');
        $('#settingsDefaultBranch').val(config.default_branch || 'main');
        $('#settingsDefaultPriority').val(config.default_priority || 2);
        $('#settingsAutoArchive').val(config.auto_archive_days || 0);

        // Set theme radio button based on saved preference
        const savedTheme = getSavedTheme();
        $('input[name="themeChoice"]').prop('checked', false);
        $('input[name="themeChoice"][value="' + savedTheme + '"]').prop('checked', true);

        // Reset token input to password type
        $('#settingsGithubToken').attr('type', 'password');
        $('#btnToggleToken').text('Show');

        // Reset to first tab
        $('.settings-tab').removeClass('active');
        $('.settings-tab[data-tab="general"]').addClass('active');
        $('.settings-content').removeClass('active');
        $('#settings-general').addClass('active');

        // Clear GitHub status
        $('#settingsGithubStatus').addClass('hidden');

        $('#settingsModal').addClass('active');
    }

    function closeSettingsModal() {
        $('#settingsModal').removeClass('active');
    }

    function validateSettingsToken() {
        const token = $('#settingsGithubToken').val().trim();
        if (!token) {
            showToast('Please enter token', 'error');
            return;
        }

        // Temporarily save and validate
        $.ajax({
            url: '/api/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify({ github_token: token })
        })
        .done(function() {
            $.post('/api/github/validate')
                .done(function(data) {
                    $('#settingsGithubStatus')
                        .removeClass('hidden error')
                        .addClass('success')
                        .html('<span class="github-status-icon">&#10003;</span>' +
                              '<span>Connected as <strong>' + escapeHtml(data.username) + '</strong></span>');
                    // Update user info
                    updateUserProfile(data);
                })
                .fail(function(xhr) {
                    $('#settingsGithubStatus')
                        .removeClass('hidden success')
                        .addClass('error')
                        .html('<span class="github-status-icon">&#10060;</span>' +
                              '<span>Token invalid</span>');
                });
        });
    }

    // ============================================================================
    // User Profile Functions
    // ============================================================================

    function checkGithubConnection() {
        if (!config.github_token) {
            updateUserProfile(null);
            return;
        }

        $.post('/api/github/validate')
            .done(function(data) {
                githubUser = data;
                updateUserProfile(data);
            })
            .fail(function() {
                githubUser = null;
                updateUserProfile(null);
            });
    }

    function updateUserProfile(user) {
        githubUser = user;
        const $avatar = $('#userAvatar');
        const $name = $('#userDropdownName');
        const $link = $('#userDropdownLink');
        const $connectBtn = $('#btnConnectGithub');
        const $disconnectBtn = $('#btnDisconnectGithub');

        if (user && user.username) {
            // User is connected
            $name.text(user.username);

            if (user.avatar_url) {
                $avatar.html('<img src="' + escapeHtml(user.avatar_url) + '" alt="Avatar">');
            } else {
                $avatar.html('<svg class="user-icon-default" viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>');
            }

            $link.attr('href', 'https://github.com/' + user.username).removeClass('hidden');
            $connectBtn.addClass('hidden');
            $disconnectBtn.removeClass('hidden');
        } else {
            // User is not connected
            $name.text('Not connected');
            $avatar.html('<svg class="user-icon-default" viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>');
            $link.addClass('hidden');
            $connectBtn.removeClass('hidden');
            $disconnectBtn.addClass('hidden');
        }
    }

    function disconnectGithub() {
        $.ajax({
            url: '/api/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify({ github_token: '' })
        })
        .done(function(data) {
            config = data;
            githubUser = null;
            updateUserProfile(null);
            showToast('GitHub connection disconnected', 'success');
            $('#userProfile').removeClass('open');
        })
        .fail(function(xhr) {
            showToast('Error disconnecting', 'error');
        });
    }

    // ============================================================================
    // Create PR Modal Functions
    // ============================================================================

    let currentPRProjectId = null;

    // Event Listeners for Create PR Modal
    $('#btnCreatePR').click(function() {
        openCreatePRModal();
    });

    $('.create-pr-close, #btnCancelPR').click(function() {
        closeCreatePRModal();
    });

    $('#createPRModal').click(function(e) {
        if (e.target === this) {
            closeCreatePRModal();
        }
    });

    $('#prProjectSelect').change(function() {
        const projectId = $(this).val();
        if (projectId) {
            loadProjectBranches(projectId);
        } else {
            $('#prFromBranch').html('<option value="">Select branch...</option>');
            $('#prToBranch').html('<option value="">Select branch...</option>');
        }
    });

    $('#prFromBranch').change(function() {
        const fromBranch = $(this).val();
        if (fromBranch) {
            // Auto-generate title from branch name
            const cleanBranch = fromBranch.replace('origin/', '').replace('working/', '');
            const parts = cleanBranch.split('-');
            // Skip the ID part if it looks like "abc12345-feature-name"
            if (parts.length > 1 && parts[0].length === 8 && /^[a-f0-9]+$/.test(parts[0])) {
                parts.shift();
            }
            const title = parts.map(p => p.charAt(0).toUpperCase() + p.slice(1)).join(' ');
            if (!$('#prTitle').val()) {
                $('#prTitle').val(title);
            }
        }
    });

    $('#btnConfirmPR').click(function() {
        createPullRequest();
    });

    function openCreatePRModal() {
        // Reset state
        currentPRProjectId = null;
        $('#prStatus').addClass('hidden');
        $('#prResult').addClass('hidden');
        $('#prError').addClass('hidden');
        $('#prTitle').val('');
        $('#btnConfirmPR').prop('disabled', false).text('Create PR');

        // Populate project dropdown with git-enabled projects
        const $projectSelect = $('#prProjectSelect');
        $projectSelect.html('<option value="">Select a project...</option>');

        const gitProjects = projects.filter(p => p.is_git_repo);
        gitProjects.forEach(function(p) {
            $projectSelect.append(`<option value="${p.id}">${escapeHtml(p.name)}</option>`);
        });

        // Pre-select current project filter if it's a git repo
        if (selectedProjectFilter) {
            const selectedProject = projects.find(p => p.id === selectedProjectFilter && p.is_git_repo);
            if (selectedProject) {
                $projectSelect.val(selectedProjectFilter);
                loadProjectBranches(selectedProjectFilter);
            }
        }

        // Reset branch dropdowns
        $('#prFromBranch').html('<option value="">Select branch...</option>');
        $('#prToBranch').html('<option value="">Select branch...</option>');

        $('#createPRModal').addClass('active');
    }

    function closeCreatePRModal() {
        $('#createPRModal').removeClass('active');
        currentPRProjectId = null;
    }

    function loadProjectBranches(projectId) {
        currentPRProjectId = projectId;
        const $fromSelect = $('#prFromBranch');
        const $toSelect = $('#prToBranch');

        $fromSelect.html('<option value="">Loading branches...</option>');
        $toSelect.html('<option value="">Loading branches...</option>');

        // Get branches for this project
        $.get('/api/projects/' + projectId + '/branches')
            .done(function(data) {
                const branches = data.branches || [];

                // Get current branch info
                $.get('/api/projects/' + projectId + '/git-info')
                    .done(function(gitInfo) {
                        const currentBranch = gitInfo.current_branch || '';

                        // Populate From Branch dropdown (only local branches)
                        $fromSelect.html('<option value="">Select branch...</option>');
                        branches.forEach(function(branch) {
                            // Skip remote branches for "From" - we only want local branches
                            if (branch.startsWith('origin/')) {
                                return;
                            }
                            let displayName = branch;
                            let isSelected = branch === currentBranch;
                            $fromSelect.append(`<option value="${escapeHtml(branch)}" ${isSelected ? 'selected' : ''}>${escapeHtml(displayName)}</option>`);
                        });

                        // Populate To Branch dropdown (typically main/master)
                        $toSelect.html('<option value="">Select branch...</option>');
                        // Add main branches first
                        const mainBranches = ['main', 'master', 'develop', 'staging'];
                        const addedBranches = new Set();

                        mainBranches.forEach(function(mainBranch) {
                            // Check if branch exists in the list
                            const found = branches.find(b => b === mainBranch || b === 'origin/' + mainBranch);
                            if (found && !addedBranches.has(mainBranch)) {
                                const isMain = mainBranch === 'main' || mainBranch === 'master';
                                $toSelect.append(`<option value="${escapeHtml(found)}" ${isMain ? 'selected' : ''}>${escapeHtml(mainBranch)}</option>`);
                                addedBranches.add(mainBranch);
                            }
                        });

                        // Add other branches
                        branches.forEach(function(branch) {
                            const cleanName = branch.replace('origin/', '');
                            if (!addedBranches.has(cleanName)) {
                                $toSelect.append(`<option value="${escapeHtml(branch)}">${escapeHtml(branch)}</option>`);
                                addedBranches.add(cleanName);
                            }
                        });

                        // Trigger change to auto-fill title
                        $fromSelect.trigger('change');
                    })
                    .fail(function() {
                        // Fallback if git-info fails
                        populateBranchDropdownsWithoutCurrentBranch(branches);
                    });
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Failed to load branches';
                $fromSelect.html('<option value="">Error loading branches</option>');
                $toSelect.html('<option value="">Error loading branches</option>');
                showToast(msg, 'error');
            });
    }

    function populateBranchDropdownsWithoutCurrentBranch(branches) {
        const $fromSelect = $('#prFromBranch');
        const $toSelect = $('#prToBranch');

        $fromSelect.html('<option value="">Select branch...</option>');
        $toSelect.html('<option value="">Select branch...</option>');

        branches.forEach(function(branch) {
            $fromSelect.append(`<option value="${escapeHtml(branch)}">${escapeHtml(branch)}</option>`);
            $toSelect.append(`<option value="${escapeHtml(branch)}">${escapeHtml(branch)}</option>`);
        });

        // Try to auto-select main/master for To Branch
        const mainBranch = branches.find(b => b === 'main' || b === 'origin/main');
        const masterBranch = branches.find(b => b === 'master' || b === 'origin/master');
        if (mainBranch) {
            $toSelect.val(mainBranch);
        } else if (masterBranch) {
            $toSelect.val(masterBranch);
        }
    }

    function createPullRequest() {
        const projectId = $('#prProjectSelect').val();
        const fromBranch = $('#prFromBranch').val();
        const toBranch = $('#prToBranch').val();
        const title = $('#prTitle').val();

        if (!projectId) {
            showToast('Please select a project', 'error');
            return;
        }
        if (!fromBranch || !toBranch) {
            showToast('Please select both branches', 'error');
            return;
        }
        if (fromBranch === toBranch) {
            showToast('From and To branches must be different', 'error');
            return;
        }

        // Show loading state
        $('#prStatus').removeClass('hidden');
        $('#prResult').addClass('hidden');
        $('#prError').addClass('hidden');
        $('#btnConfirmPR').prop('disabled', true).text('Creating...');

        $.ajax({
            url: '/api/github/create-pr',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({
                project_id: projectId,
                from_branch: fromBranch,
                to_branch: toBranch,
                title: title
            })
        })
        .done(function(data) {
            $('#prStatus').addClass('hidden');

            if (data.success) {
                // Show success result
                $('#prResult').removeClass('hidden');

                if (data.existing) {
                    $('.pr-success-text').text('PR #' + data.pr_number + ' already exists');
                } else {
                    $('.pr-success-text').text('PR #' + data.pr_number + ' created successfully!');
                }

                $('#prResultLink').attr('href', data.pr_url).text('View PR #' + data.pr_number);
                $('#btnConfirmPR').prop('disabled', true).text('Done');

                showToast(data.message || 'PR created successfully!', 'success');
            } else {
                // Show error
                $('#prError').removeClass('hidden');

                let errorMessage = data.error || 'Failed to create PR';
                if (data.error_type === 'auth') {
                    errorMessage = 'GitHub authentication failed. Please check your token in Settings.';
                } else if (data.error_type === 'uncommitted') {
                    errorMessage = 'You have uncommitted changes. Please commit your changes before creating a PR.';
                } else if (data.error_type === 'identical') {
                    errorMessage = 'No commits to merge. The source branch has no new commits compared to the target branch.';
                }

                $('#prError .pr-error-text').text(errorMessage);
                $('#btnConfirmPR').prop('disabled', false).text('Create PR');

                showToast(errorMessage, 'error');
            }
        })
        .fail(function(xhr) {
            $('#prStatus').addClass('hidden');
            $('#prError').removeClass('hidden');

            const msg = xhr.responseJSON?.error || 'Failed to create PR';
            $('#prError .pr-error-text').text(msg);
            $('#btnConfirmPR').prop('disabled', false).text('Create PR');

            showToast(msg, 'error');
        });
    }

    // ============================================================================
    // ATTACHMENT HANDLING
    // ============================================================================

    // Load attachments for a task
    function loadAttachments(taskId) {
        currentAttachments = [];
        clearAttachmentList();

        if (!taskId) return;

        $.get('/api/tasks/' + taskId + '/attachments')
            .done(function(attachments) {
                currentAttachments = attachments || [];
                renderAttachmentList();
            })
            .fail(function() {
                console.log('Failed to load attachments');
            });
    }

    // Clear the attachment list UI
    function clearAttachmentList() {
        $('#attachmentList').empty();
        currentAttachments = [];
    }

    // Render the attachment list
    function renderAttachmentList() {
        const $list = $('#attachmentList');
        $list.empty();

        currentAttachments.forEach(function(attachment, index) {
            const isImage = attachment.mime_type.startsWith('image/');
            const isVideo = attachment.mime_type.startsWith('video/');
            const sizeStr = formatFileSize(attachment.size);

            let thumbnailHtml = '';
            if (isImage) {
                thumbnailHtml = `<img class="attachment-thumbnail" src="/uploads/${attachment.task_id}/${attachment.path.split('/').pop()}" alt="${escapeHtml(attachment.filename)}">`;
            } else if (isVideo) {
                thumbnailHtml = `
                    <div class="attachment-video-thumbnail">
                        <video src="/uploads/${attachment.task_id}/${attachment.path.split('/').pop()}" muted></video>
                        <div class="video-play-overlay">
                            <svg viewBox="0 0 24 24" fill="currentColor">
                                <path d="M8 5v14l11-7z"/>
                            </svg>
                        </div>
                    </div>
                `;
            }

            const $item = $(`
                <div class="attachment-item" data-id="${attachment.id}" data-index="${index}">
                    ${thumbnailHtml}
                    <div class="attachment-info">
                        <span class="attachment-name" title="${escapeHtml(attachment.filename)}">${escapeHtml(attachment.filename)}</span>
                        <span class="attachment-size">${sizeStr}</span>
                    </div>
                    <button class="attachment-delete" data-id="${attachment.id}" title="Remove">&times;</button>
                </div>
            `);

            $list.append($item);
        });
    }

    // Format file size
    function formatFileSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    }

    // Upload a file
    function uploadFile(file, taskId) {
        const formData = new FormData();
        formData.append('file', file);

        $.ajax({
            url: '/api/tasks/' + taskId + '/attachments',
            type: 'POST',
            data: formData,
            processData: false,
            contentType: false
        })
        .done(function(attachment) {
            currentAttachments.push(attachment);
            renderAttachmentList();
            showToast('File uploaded', 'success');
            // Update task in local state
            const taskIndex = tasks.findIndex(t => t.id === taskId);
            if (taskIndex !== -1) {
                if (!tasks[taskIndex].attachments) tasks[taskIndex].attachments = [];
                tasks[taskIndex].attachments.push(attachment);
            }
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Upload failed';
            showToast(msg, 'error');
        });
    }

    // Delete an attachment
    function deleteAttachment(attachmentId, taskId) {
        $.ajax({
            url: '/api/tasks/' + taskId + '/attachments/' + attachmentId,
            type: 'DELETE'
        })
        .done(function() {
            currentAttachments = currentAttachments.filter(a => a.id !== attachmentId);
            renderAttachmentList();
            showToast('Attachment deleted', 'success');
            // Update task in local state
            const taskIndex = tasks.findIndex(t => t.id === taskId);
            if (taskIndex !== -1 && tasks[taskIndex].attachments) {
                tasks[taskIndex].attachments = tasks[taskIndex].attachments.filter(a => a.id !== attachmentId);
            }
        })
        .fail(function() {
            showToast('Failed to delete attachment', 'error');
        });
    }

    // Open lightbox
    function openLightbox(index) {
        if (currentAttachments.length === 0) return;

        lightboxIndex = index;
        showLightboxItem(index);
        $('#lightbox').removeClass('hidden');

        // Add keyboard listener
        $(document).on('keydown.lightbox', function(e) {
            if (e.key === 'Escape') closeLightbox();
            if (e.key === 'ArrowLeft') showPrevLightboxItem();
            if (e.key === 'ArrowRight') showNextLightboxItem();
        });
    }

    // Close lightbox
    function closeLightbox() {
        $('#lightbox').addClass('hidden');
        $('#lightboxVideo').get(0)?.pause();
        $(document).off('keydown.lightbox');
    }

    // Show lightbox item
    function showLightboxItem(index) {
        const attachment = currentAttachments[index];
        if (!attachment) return;

        const isVideo = attachment.mime_type.startsWith('video/');
        const url = '/uploads/' + attachment.task_id + '/' + attachment.path.split('/').pop();

        if (isVideo) {
            $('#lightboxImage').addClass('hidden');
            $('#lightboxVideo').removeClass('hidden').attr('src', url);
        } else {
            $('#lightboxVideo').addClass('hidden').get(0)?.pause();
            $('#lightboxImage').removeClass('hidden').attr('src', url);
        }

        $('#lightboxFilename').text(attachment.filename);
        $('#lightboxCounter').text((index + 1) + ' / ' + currentAttachments.length);

        // Show/hide navigation buttons
        if (currentAttachments.length <= 1) {
            $('.lightbox-prev, .lightbox-next').hide();
        } else {
            $('.lightbox-prev, .lightbox-next').show();
        }
    }

    // Navigate lightbox
    function showPrevLightboxItem() {
        lightboxIndex = (lightboxIndex - 1 + currentAttachments.length) % currentAttachments.length;
        showLightboxItem(lightboxIndex);
    }

    function showNextLightboxItem() {
        lightboxIndex = (lightboxIndex + 1) % currentAttachments.length;
        showLightboxItem(lightboxIndex);
    }

    // ============================================================================
    // ATTACHMENT EVENT HANDLERS
    // ============================================================================

    // Drag and drop zone
    const $dropZone = $('#attachmentDropZone');

    $dropZone.on('dragover', function(e) {
        e.preventDefault();
        e.stopPropagation();
        $(this).addClass('drag-over');
    });

    $dropZone.on('dragleave', function(e) {
        e.preventDefault();
        e.stopPropagation();
        $(this).removeClass('drag-over');
    });

    $dropZone.on('drop', function(e) {
        e.preventDefault();
        e.stopPropagation();
        $(this).removeClass('drag-over');

        const files = e.originalEvent.dataTransfer.files;
        handleFileUpload(files);
    });

    // File input change
    $('#attachmentInput').on('change', function() {
        const files = this.files;
        handleFileUpload(files);
        this.value = ''; // Reset input
    });

    // Handle file upload
    function handleFileUpload(files) {
        for (let i = 0; i < files.length; i++) {
            const file = files[i];

            // Validate file type
            const allowedTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm', 'video/quicktime'];
            if (!allowedTypes.includes(file.type)) {
                showToast('File type not allowed: ' + file.name, 'error');
                continue;
            }

            // Validate file size (50MB)
            if (file.size > 50 * 1024 * 1024) {
                showToast('File too large (max 50MB): ' + file.name, 'error');
                continue;
            }

            if (currentTaskId) {
                // Upload immediately for existing tasks
                uploadFile(file, currentTaskId);
            } else {
                // Add to pending list for new tasks
                addPendingAttachment(file);
            }
        }
    }

    // Add file to pending attachments list (for new tasks before save)
    function addPendingAttachment(file) {
        // Create preview data URL
        const reader = new FileReader();
        reader.onload = function(e) {
            const pendingItem = {
                file: file,
                name: file.name,
                type: file.type,
                size: file.size,
                dataUrl: e.target.result
            };
            pendingAttachments.push(pendingItem);
            renderPendingAttachments();
        };
        reader.readAsDataURL(file);
    }

    // Render pending attachments preview
    function renderPendingAttachments() {
        const $list = $('#pendingAttachmentList');
        $list.empty();

        pendingAttachments.forEach((item, index) => {
            const isImage = item.type.startsWith('image/');
            const isVideo = item.type.startsWith('video/');

            let preview = '';
            if (isImage) {
                preview = `<img src="${item.dataUrl}" alt="${escapeHtml(item.name)}">`;
            } else if (isVideo) {
                preview = `<video src="${item.dataUrl}" muted></video>`;
            }

            $list.append(`
                <div class="attachment-item" data-index="${index}">
                    ${preview}
                    <button class="attachment-delete pending-delete" data-index="${index}">&times;</button>
                    <div class="attachment-name">${escapeHtml(item.name)}</div>
                </div>
            `);
        });
    }

    // Clear pending attachments
    function clearPendingAttachments() {
        pendingAttachments = [];
        $('#pendingAttachmentList').empty();
    }

    // Upload pending attachments after task is created
    function uploadPendingAttachments(taskId) {
        if (pendingAttachments.length === 0) return;

        pendingAttachments.forEach(item => {
            uploadFile(item.file, taskId);
        });

        clearPendingAttachments();
    }

    // Delete attachment button
    $(document).on('click', '.attachment-delete', function(e) {
        e.stopPropagation();

        // Check if this is a pending attachment delete
        if ($(this).hasClass('pending-delete')) {
            const index = $(this).data('index');
            pendingAttachments.splice(index, 1);
            renderPendingAttachments();
            return;
        }

        const attachmentId = $(this).data('id');
        if (currentTaskId && confirm('Delete this attachment?')) {
            deleteAttachment(attachmentId, currentTaskId);
        }
    });

    // Click attachment to open lightbox
    $(document).on('click', '.attachment-item', function(e) {
        if ($(e.target).hasClass('attachment-delete')) return;
        const index = $(this).data('index');
        openLightbox(index);
    });

    // Lightbox controls
    $('.lightbox-close').on('click', closeLightbox);
    $('.lightbox-prev').on('click', showPrevLightboxItem);
    $('.lightbox-next').on('click', showNextLightboxItem);

    // Close lightbox on background click
    $('#lightbox').on('click', function(e) {
        if (e.target === this) {
            closeLightbox();
        }
    });

    // ============================================================================
    // GLOBAL SEARCH (Cmd+K / Ctrl+K)
    // ============================================================================

    let searchSelectedIndex = -1;
    let searchFilteredTasks = [];

    // Open search modal
    function openSearch() {
        $('#searchInput').val('');
        $('#searchResults').empty();
        $('#searchEmpty').addClass('hidden');
        searchSelectedIndex = -1;
        searchFilteredTasks = [];
        $('#searchModal').addClass('active');
        // Focus input after modal opens
        setTimeout(() => $('#searchInput').focus(), 50);
    }

    // Close search modal
    function closeSearch() {
        $('#searchModal').removeClass('active');
        searchSelectedIndex = -1;
        searchFilteredTasks = [];
    }

    // Get project name by ID for search
    function getProjectNameForSearch(projectId) {
        if (!projectId) return 'No project';
        const project = projects.find(p => p.id === projectId);
        return project ? project.name : 'Unknown';
    }

    // Get task type info by ID for search
    function getTaskTypeInfoForSearch(taskTypeId) {
        if (!taskTypeId) return null;
        return taskTypes.find(t => t.id === taskTypeId);
    }

    // Filter and render search results
    function filterSearch(query) {
        const $results = $('#searchResults');
        const $empty = $('#searchEmpty');
        $results.empty();

        const normalizedQuery = query.toLowerCase().trim();

        if (!normalizedQuery) {
            $empty.addClass('hidden');
            searchFilteredTasks = [];
            searchSelectedIndex = -1;
            return;
        }

        // Search in title, description, and task type name
        searchFilteredTasks = tasks.filter(task => {
            const title = (task.title || '').toLowerCase();
            const description = (task.description || '').toLowerCase();
            const taskType = getTaskTypeInfoForSearch(task.task_type_id);
            const taskTypeName = taskType ? taskType.name.toLowerCase() : '';

            return title.includes(normalizedQuery) ||
                   description.includes(normalizedQuery) ||
                   taskTypeName.includes(normalizedQuery);
        });

        // Limit to 10 results
        searchFilteredTasks = searchFilteredTasks.slice(0, 10);

        if (searchFilteredTasks.length === 0) {
            $empty.removeClass('hidden');
            searchSelectedIndex = -1;
            return;
        }

        $empty.addClass('hidden');
        searchSelectedIndex = 0;

        // Render results
        searchFilteredTasks.forEach((task, index) => {
            const taskType = getTaskTypeInfoForSearch(task.task_type_id);
            const projectName = getProjectNameForSearch(task.project_id);

            // Build type badge if task type exists
            let typeBadge = '';
            if (taskType) {
                const typeColor = taskType.color || '#8b949e';
                typeBadge = `<span class="search-result-type" style="color: ${typeColor}; border: 1px solid ${typeColor}33;">${escapeHtml(taskType.name)}</span>`;
            }

            const selectedClass = index === 0 ? 'selected' : '';
            const statusLabel = task.status === 'progress' ? 'In Progress' : task.status;

            $results.append(`
                <div class="search-result-item ${selectedClass}" data-task-id="${task.id}" data-index="${index}">
                    <div class="search-result-content">
                        <div class="search-result-title">${escapeHtml(task.title)}</div>
                        <div class="search-result-meta">
                            <span class="search-result-project">${escapeHtml(projectName)}</span>
                        </div>
                    </div>
                    <div class="search-result-badges">
                        ${typeBadge}
                        <span class="search-result-status ${task.status}">${statusLabel}</span>
                    </div>
                </div>
            `);
        });
    }

    // Update selected result highlight
    function updateSearchSelection() {
        $('.search-result-item').removeClass('selected');
        if (searchSelectedIndex >= 0 && searchSelectedIndex < searchFilteredTasks.length) {
            const $item = $(`.search-result-item[data-index="${searchSelectedIndex}"]`);
            $item.addClass('selected');
            // Scroll into view if needed
            const container = $('#searchResults')[0];
            const item = $item[0];
            if (item && container) {
                const itemTop = item.offsetTop;
                const itemBottom = itemTop + item.offsetHeight;
                const containerTop = container.scrollTop;
                const containerBottom = containerTop + container.clientHeight;

                if (itemTop < containerTop) {
                    container.scrollTop = itemTop;
                } else if (itemBottom > containerBottom) {
                    container.scrollTop = itemBottom - container.clientHeight;
                }
            }
        }
    }

    // Open task from search result
    function openSearchResult(index) {
        if (index >= 0 && index < searchFilteredTasks.length) {
            const task = searchFilteredTasks[index];
            closeSearch();
            openEditTaskModal(task);
            $('#taskModal').addClass('active');
        }
    }

    // Global keyboard shortcut for Cmd+K / Ctrl+K
    $(document).on('keydown', function(e) {
        // Cmd+K (Mac) or Ctrl+K (Windows/Linux)
        if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
            e.preventDefault();
            if ($('#searchModal').hasClass('active')) {
                closeSearch();
            } else {
                openSearch();
            }
            return;
        }

        // Only handle these keys when search modal is open
        if (!$('#searchModal').hasClass('active')) return;

        // Escape to close
        if (e.key === 'Escape') {
            e.preventDefault();
            closeSearch();
            return;
        }

        // Arrow down to move selection down
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (searchFilteredTasks.length > 0) {
                searchSelectedIndex = Math.min(searchSelectedIndex + 1, searchFilteredTasks.length - 1);
                updateSearchSelection();
            }
            return;
        }

        // Arrow up to move selection up
        if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (searchFilteredTasks.length > 0) {
                searchSelectedIndex = Math.max(searchSelectedIndex - 1, 0);
                updateSearchSelection();
            }
            return;
        }

        // Enter to open selected result
        if (e.key === 'Enter') {
            e.preventDefault();
            openSearchResult(searchSelectedIndex);
            return;
        }
    });

    // Search input handler
    $('#searchInput').on('input', function() {
        const query = $(this).val();
        filterSearch(query);
    });

    // Click on search result
    $(document).on('click', '.search-result-item', function() {
        const index = $(this).data('index');
        openSearchResult(index);
    });

    // Hover on search result to update selection
    $(document).on('mouseenter', '.search-result-item', function() {
        searchSelectedIndex = $(this).data('index');
        updateSearchSelection();
    });

    // Click outside search modal content to close
    $('#searchModal').on('click', function(e) {
        if (e.target === this) {
            closeSearch();
        }
    });
});

// ============================================================================
// GLOBAL FUNCTIONS FOR LOG INTERACTION
// ============================================================================

// Toggle iteration collapse state
function toggleIteration(iterNum) {
    const $block = $(`.iteration-block[data-iteration="${iterNum}"]`);
    $block.toggleClass('collapsed');
}

// Toggle output expand/collapse
function toggleOutputExpand(btn) {
    const $btn = $(btn);
    const $outputContent = $btn.closest('.output-block').find('.output-content');
    const isCollapsed = $outputContent.hasClass('collapsed');

    if (isCollapsed) {
        // Expand - show full content
        const fullContent = $outputContent.data('full');
        $outputContent.text(fullContent);
        $outputContent.removeClass('collapsed');
        $btn.text('Show less ▲');
    } else {
        // Collapse - show preview
        const fullContent = $outputContent.data('full');
        const lines = fullContent.split('\n');
        const preview = lines.slice(0, 10).join('\n');
        $outputContent.text(preview);
        $outputContent.addClass('collapsed');
        $btn.text('Show more ▼');
    }
}
