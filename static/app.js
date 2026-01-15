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
                showToast('Fehler beim Laden der Konfiguration', 'error');
            });
    }

    function loadProjects() {
        $.get('/api/projects')
            .done(function(data) {
                projects = data || [];
                renderProjectList();
                populateProjectSelect();
            })
            .fail(function(xhr) {
                showToast('Fehler beim Laden der Projekte', 'error');
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
                showToast('Fehler beim Laden der Task-Typen', 'error');
            });
    }

    function loadTasks() {
        $.get('/api/tasks')
            .done(function(data) {
                tasks = data || [];
                renderAllTasks();
            })
            .fail(function(xhr) {
                showToast('Fehler beim Laden der Tasks', 'error');
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
            closeModal();
            showToast(isNew ? 'Task erstellt' : 'Task gespeichert', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Speichern';
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
            showToast('Task geloescht', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Loeschen';
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

            if (newStatus === 'progress') {
                openEditTaskModal(task);
            }
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Aktualisieren';
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
            auto_commit: $('#settingsAutoCommit').is(':checked'),
            auto_push: $('#settingsAutoPush').is(':checked'),
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
            showToast('Einstellungen gespeichert', 'success');
            closeSettingsModal();
            // Re-check GitHub connection after saving
            checkGithubConnection();
        })
        .fail(function(xhr) {
            showToast('Fehler beim Speichern der Einstellungen', 'error');
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
            showToast(isNew ? 'Projekt erstellt' : 'Projekt gespeichert', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Speichern';
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
            showToast('Projekt geloescht', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Loeschen';
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
            const msg = xhr.responseJSON?.error || 'Fehler beim Hinzufuegen';
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
            const msg = xhr.responseJSON?.error || 'Fehler beim Loeschen';
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
            showToast(isNew ? 'Task-Typ erstellt' : 'Task-Typ gespeichert', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Speichern';
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
            showToast('Task-Typ geloescht', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Loeschen';
            showToast(msg, 'error');
        });
    }

    // Scan Projects
    function scanProjects(basePath, maxDepth) {
        $('#btnStartScan').prop('disabled', true).text('Scanne...');
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
            const msg = xhr.responseJSON?.error || 'Fehler beim Scannen';
            showToast(msg, 'error');
        })
        .always(function() {
            $('#btnStartScan').prop('disabled', false).text('Scannen');
        });
    }

    function importScannedProjects() {
        const selected = [];
        $('#scanResultsList input:checked').each(function() {
            selected.push($(this).data('path'));
        });

        if (selected.length === 0) {
            showToast('Keine Repositories ausgewaehlt', 'error');
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
                showToast(`${imported} Projekte importiert` + (failed > 0 ? `, ${failed} fehlgeschlagen` : ''), imported > 0 ? 'success' : 'error');
            }
        }
    }

    // RALPH Control Functions
    function pauseTask(taskId) {
        $.post('/api/tasks/' + taskId + '/pause')
            .done(function() {
                $('#btnPause').addClass('hidden');
                $('#btnResume').removeClass('hidden');
                showToast('Prozess pausiert', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Fehler beim Pausieren';
                showToast(msg, 'error');
            });
    }

    function resumeTask(taskId) {
        $.post('/api/tasks/' + taskId + '/resume')
            .done(function() {
                $('#btnResume').addClass('hidden');
                $('#btnPause').removeClass('hidden');
                showToast('Prozess fortgesetzt', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Fehler beim Fortsetzen';
                showToast(msg, 'error');
            });
    }

    function stopTask(taskId) {
        $.post('/api/tasks/' + taskId + '/stop')
            .done(function() {
                showToast('Prozess gestoppt', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Fehler beim Stoppen';
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
            showToast('Feedback gesendet', 'success');
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Senden';
            showToast(msg, 'error');
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
            case 'status':
                updateStatusBadge(msg.task_id, msg.status, msg.iteration);
                break;
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
        }
    }

    function showDeploymentSuccess(taskId, message) {
        // Find the task and show success animation
        const task = tasks.find(t => t.id === taskId);
        const taskTitle = task ? task.title : 'Task';
        showToast(`Deployed: ${taskTitle}`, 'success');

        // Trigger rocket animation on Done column
        const $doneColumn = $('.column[data-status="done"]');
        const $rocketIcon = $doneColumn.find('.column-rocket');
        if ($rocketIcon.length) {
            $rocketIcon.addClass('launching');
            setTimeout(() => $rocketIcon.removeClass('launching'), 1500);
        }
    }

    function appendLog(taskId, message) {
        if (currentTaskId !== taskId) return;

        const $log = $('#logOutput');
        const formatted = formatLogMessage(message);

        if (formatted) {
            const $newLine = $('<div class="log-entry new-line"></div>').html(formatted);
            $log.append($newLine);

            setTimeout(function() {
                $newLine.removeClass('new-line');
            }, 1000);

            if (autoScroll) {
                scrollToBottom($log);
            }
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

    function formatLogMessage(message) {
        if (message.startsWith('[GRINDER]')) {
            return `<span class="log-system">${escapeHtml(message)}</span>`;
        }

        try {
            const data = JSON.parse(message.trim());
            return formatJsonLog(data);
        } catch (e) {
            const trimmed = message.trim();
            if (trimmed) {
                return `<span class="log-text">${escapeHtml(trimmed)}</span>`;
            }
            return null;
        }
    }

    function formatJsonLog(data) {
        switch (data.type) {
            case 'system':
                if (data.subtype === 'init') {
                    return `<div class="log-init">
                        <span class="log-icon">&#128640;</span>
                        <span>Claude gestartet in <code>${data.cwd}</code></span>
                    </div>`;
                }
                return null;

            case 'assistant':
                const msg = data.message;
                if (!msg || !msg.content) return null;

                let html = '';
                for (const block of msg.content) {
                    if (block.type === 'text' && block.text) {
                        html += `<div class="log-assistant">
                            <span class="log-icon">&#129302;</span>
                            <span>${escapeHtml(block.text)}</span>
                        </div>`;
                    }
                    if (block.type === 'tool_use') {
                        const toolName = block.name;
                        let toolInfo = '';

                        if (toolName === 'Write' || toolName === 'Edit') {
                            toolInfo = block.input?.file_path || '';
                        } else if (toolName === 'Bash') {
                            toolInfo = block.input?.description || block.input?.command?.substring(0, 50) || '';
                        } else if (toolName === 'Read') {
                            toolInfo = block.input?.file_path || '';
                        } else if (toolName === 'TodoWrite') {
                            toolInfo = 'Updating task list...';
                        }

                        html += `<div class="log-tool">
                            <span class="log-icon">&#128295;</span>
                            <span class="tool-name">${toolName}</span>
                            <span class="tool-info">${escapeHtml(toolInfo)}</span>
                        </div>`;
                    }
                }
                return html || null;

            case 'user':
                const content = data.message?.content;
                if (!content || !Array.isArray(content)) return null;

                for (const block of content) {
                    if (block.type === 'tool_result') {
                        const result = block.content || '';
                        const isError = block.is_error;
                        const preview = result.length > 200 ? result.substring(0, 200) + '...' : result;

                        if (isError) {
                            return `<div class="log-error">
                                <span class="log-icon">&#10060;</span>
                                <span>${escapeHtml(preview)}</span>
                            </div>`;
                        }

                        if (result.includes('successfully') || result.includes('Error') || result.length < 100) {
                            return `<div class="log-result">
                                <span class="log-icon">&#10003;</span>
                                <span>${escapeHtml(preview)}</span>
                            </div>`;
                        }
                    }
                }
                return null;

            default:
                return null;
        }
    }

    function updateStatusBadge(taskId, status, iteration) {
        const $card = $(`.task-card[data-id="${taskId}"]`);
        const $badge = $card.find('.status-badge');

        if (status === 'progress' && iteration > 0) {
            if ($badge.length === 0) {
                $card.find('.task-card-footer').prepend(
                    `<span class="status-badge running">Iteration ${iteration}</span>`
                );
            } else {
                $badge.text('Iteration ' + iteration);
            }
        }

        if (currentTaskId === taskId) {
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

    // Rendering
    function renderAllTasks() {
        const statuses = ['backlog', 'progress', 'review', 'done', 'blocked'];
        statuses.forEach(function(status) {
            const $container = $(`.column[data-status="${status}"] .tasks-container`);
            $container.empty();

            let statusTasks = tasks.filter(t => t.status === status);

            // Filter by project if selected
            if (selectedProjectFilter) {
                statusTasks = statusTasks.filter(t => t.project_id === selectedProjectFilter);
            }

            statusTasks.forEach(function(task) {
                $container.append(createTaskCard(task));
            });
        });
    }

    function createTaskCard(task) {
        const taskType = task.task_type || taskTypes.find(t => t.id === task.task_type_id);
        const typeBadge = taskType ?
            `<span class="task-type-badge" style="background-color: ${taskType.color}">${escapeHtml(taskType.name)}</span>` : '';
        // Show shortened branch name with full name in tooltip
        let branchBadge = '';
        if (task.working_branch) {
            const shortBranch = task.working_branch.length > 20
                ? task.working_branch.substring(0, 17) + '...'
                : task.working_branch;
            branchBadge = `<span class="task-branch-badge" title="${escapeHtml(task.working_branch)}"><svg class="branch-icon" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25Zm-6 0a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Zm8.25-.75a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM4.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z"/></svg><span class="branch-name">${escapeHtml(shortBranch)}</span></span>`;
        }

        const $card = $(`
            <div class="task-card" data-id="${task.id}" draggable="true">
                <div class="task-card-header">
                    <div class="priority-indicator priority-${task.priority}"></div>
                    <span class="task-title">${escapeHtml(task.title)}</span>
                </div>
                ${(typeBadge || branchBadge) ? `<div class="task-card-meta">${typeBadge}${branchBadge}</div>` : ''}
                <div class="task-card-footer"></div>
            </div>
        `);

        if (task.status === 'progress') {
            const badgeText = task.current_iteration > 0
                ? `Iteration ${task.current_iteration}`
                : 'Running...';
            $card.find('.task-card-footer').prepend(
                `<span class="status-badge running">${badgeText}</span>`
            );
            $card.find('.task-card-footer').append(
                `<button class="btn-live" data-id="${task.id}">LIVE</button>`
            );
        }

        if (task.status === 'blocked') {
            $card.find('.task-card-footer').append(
                '<span class="blocked-icon">&#9888;</span>'
            );
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
                        <span class="project-name">${escapeHtml(project.name)}</span>
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
                        <span class="project-name">${escapeHtml(project.name)}</span>
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
            $list.append(`
                <div class="task-type-item" data-type-id="${type.id}">
                    <span class="task-type-color" style="background-color: ${type.color}"></span>
                    <span class="task-type-name">${escapeHtml(type.name)}</span>
                    <span class="task-type-count">${count}</span>
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
            $list.html('<span style="color: var(--text-secondary); font-size: 0.8rem;">Keine Regeln definiert</span>');
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
            $list.html('<div style="padding: 1rem; text-align: center; color: var(--text-secondary);">Keine Repositories gefunden</div>');
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

        // Token visibility toggle
        $('#btnToggleToken').on('click', function() {
            const $input = $('#settingsGithubToken');
            if ($input.attr('type') === 'password') {
                $input.attr('type', 'text');
                $(this).text('Verbergen');
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
            if (confirm('GitHub Verbindung trennen?')) {
                disconnectGithub();
            }
        });

        // Add task buttons
        $('.btn-add').on('click', function() {
            const status = $(this).data('status');
            openNewTaskModal(status);
        });

        // Task card clicks
        $(document).on('click', '.task-card', function(e) {
            if ($(e.target).hasClass('btn-live')) return;
            const taskId = $(this).data('id');
            const task = tasks.find(t => t.id === taskId);
            if (task) openEditTaskModal(task);
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

        // Project click in sidebar
        $(document).on('click', '.project-item', function(e) {
            // Check for double-click to edit
            const projectId = $(this).data('project-id');
            selectedProjectFilter = projectId;
            $('.project-item').removeClass('active');
            $(this).addClass('active');
            renderAllTasks();
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

        // Task type click in sidebar
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
            showToast('Projekte aktualisiert', 'success');
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
            if (confirm('Task wirklich loeschen?')) {
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
            if (confirm('Projekt wirklich loeschen? Alle verknuepften Tasks verlieren die Projektzuordnung.')) {
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
                showToast('Bitte Basis-Verzeichnis angeben', 'error');
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
            if (confirm('Task-Typ wirklich loeschen?')) {
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
            if (confirm('Prozess wirklich stoppen?')) {
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
            if (confirm('Git Repository initialisieren?')) {
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

        let isResizing = false;
        let startX, startWidth;

        // Load saved width from localStorage
        const savedWidth = localStorage.getItem('grinder_sidebar_width');
        if (savedWidth) {
            const width = parseInt(savedWidth);
            if (width >= 200 && width <= 500) {
                sidebar.style.width = width + 'px';
            }
        }

        resizeHandle.addEventListener('mousedown', function(e) {
            isResizing = true;
            startX = e.clientX;
            startWidth = sidebar.offsetWidth;

            resizeHandle.classList.add('dragging');
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';

            e.preventDefault();
        });

        document.addEventListener('mousemove', function(e) {
            if (!isResizing) return;

            const diff = e.clientX - startX;
            let newWidth = startWidth + diff;

            // Clamp width between min and max
            newWidth = Math.max(200, Math.min(500, newWidth));

            sidebar.style.width = newWidth + 'px';
        });

        document.addEventListener('mouseup', function() {
            if (!isResizing) return;

            isResizing = false;
            resizeHandle.classList.remove('dragging');
            document.body.style.cursor = '';
            document.body.style.userSelect = '';

            // Save width to localStorage
            localStorage.setItem('grinder_sidebar_width', sidebar.offsetWidth);
        });
    }

    // Task Modal Functions
    function openNewTaskModal(status) {
        currentTaskId = null;
        $('#modalTitle').text('Neuer Task');
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

        $('#taskModal').addClass('active');
        $('#taskTitle').focus();
    }

    function openEditTaskModal(task) {
        currentTaskId = task.id;
        $('#modalTitle').text('Task bearbeiten');
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

        // Feedback section - show for all states except backlog
        if (task.status !== 'backlog') {
            $('#feedbackSection').removeClass('hidden');

            // Update labels based on task status
            if (task.status === 'progress') {
                $('#feedbackLabel').text('Feedback an Claude:');
                $('#feedbackHelp').text('Sendet Feedback an den laufenden Prozess');
                $('#btnFeedback').text('Senden');
            } else if (task.status === 'done' || task.status === 'review') {
                $('#feedbackLabel').text('Task fortsetzen:');
                $('#feedbackHelp').text('Startet Claude erneut mit deiner Nachricht');
                $('#btnFeedback').text('Fortsetzen');
            } else if (task.status === 'blocked') {
                $('#feedbackLabel').text('Task entsperren:');
                $('#feedbackHelp').text('Startet Claude erneut mit deiner Nachricht');
                $('#btnFeedback').text('Entsperren');
            } else {
                $('#feedbackLabel').text('Nachricht an Claude:');
                $('#feedbackHelp').text('Startet Claude mit deiner Nachricht');
                $('#btnFeedback').text('Starten');
            }
        } else {
            $('#feedbackSection').addClass('hidden');
        }

        // Log section
        if (task.status === 'progress' || task.logs) {
            const wasHidden = $('#logSection').hasClass('hidden');
            $('#logSection').removeClass('hidden');

            if (task.status === 'progress' && !task.logs) {
                $('#logOutput').html('<span class="waiting">Claude is starting... waiting for output...</span>');
            } else {
                $('#logOutput').text(task.logs || '');
            }

            const badgeText = task.current_iteration > 0
                ? 'Iteration ' + task.current_iteration
                : 'Running...';
            $('#iterationBadge').text(badgeText);

            // Only initialize scroll detection once when log section first becomes visible
            if (wasHidden) {
                setupLogScrollDetection();
                // Reset auto-scroll state when first opening
                autoScroll = true;
                $('#autoScroll').prop('checked', true);
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
            showToast('Titel ist erforderlich', 'error');
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
        $('#projectModalTitle').text('Neues Projekt');
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
        $('#projectModalTitle').text('Projekt bearbeiten');
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
            showToast('Name und Pfad sind erforderlich', 'error');
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
        $('#taskTypeModalTitle').text('Neuer Task-Typ');
        $('#taskTypeId').val('');
        $('#taskTypeName').val('');
        $('#taskTypeColor').val('#58a6ff');
        $('#taskTypeColorPreview').css('background-color', '#58a6ff');
        $('#btnDeleteTaskType').addClass('hidden');
        $('#taskTypeModal').addClass('active');
    }

    function openEditTaskTypeModal(type) {
        currentTaskTypeId = type.id;
        $('#taskTypeModalTitle').text('Task-Typ bearbeiten');
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
            showToast('Name ist erforderlich', 'error');
            return;
        }

        if (currentTaskTypeId) {
            typeData.id = currentTaskTypeId;
        }

        saveTaskType(typeData);
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
                const msg = xhr.responseJSON?.error || 'Fehler beim Laden';
                showToast(msg, 'error');
            });
    }

    function renderFolderList(directories, currentIsRepo) {
        const $list = $('#folderList');
        $list.empty();

        if (!directories || directories.length === 0) {
            $list.html('<div class="folder-empty">Keine Unterordner</div>');
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
            showToast('Ordner erstellt', 'success');
            loadFolder(folderBrowserPath);
            selectedFolderPath = data.path;
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Erstellen';
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
            showToast('GitHub Token gespeichert', 'success');
            closeGithubModal();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Speichern';
            showToast(msg, 'error');
        });
    }

    function validateGithubToken() {
        const token = $('#githubToken').val().trim();
        if (!token) {
            showToast('Bitte Token eingeben', 'error');
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
                              '<span>Verbunden als <strong>' + escapeHtml(data.username) + '</strong></span>');
                })
                .fail(function(xhr) {
                    $('#githubStatus')
                        .removeClass('hidden success')
                        .addClass('error')
                        .html('<span class="github-status-icon">&#10060;</span>' +
                              '<span>Token ungueltig</span>');
                });
        });
    }

    function initializeGit(projectId) {
        $.post('/api/projects/' + projectId + '/git-init')
            .done(function(project) {
                const idx = projects.findIndex(p => p.id === project.id);
                if (idx !== -1) projects[idx] = project;
                renderProjectList();
                showToast('Git Repository initialisiert', 'success');
            })
            .fail(function(xhr) {
                const msg = xhr.responseJSON?.error || 'Fehler bei Git Init';
                showToast(msg, 'error');
            });
    }

    function createGithubRepo(projectId, repoName, description, isPrivate) {
        $('#btnCreateRepo').prop('disabled', true).text('Erstelle...');

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
            showToast('GitHub Repository erstellt: ' + data.repo_url, 'success');
            closeCreateRepoModal();
            loadProjects();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Fehler beim Erstellen';
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
            const msg = 'Deployment erfolgreich!' + (data.commit_hash ? ' Commit: ' + data.commit_hash.substring(0, 7) : '');
            showToast(msg, 'success');
            closeDeployModal();
            loadTasks();
        })
        .fail(function(xhr) {
            const msg = xhr.responseJSON?.error || 'Deployment fehlgeschlagen';
            showToast(msg, 'error');
            $('#deployStatus').addClass('hidden');
        })
        .always(function() {
            $('#btnConfirmDeploy').prop('disabled', false);
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
        $('#settingsAutoCommit').prop('checked', config.auto_commit || false);
        $('#settingsAutoPush').prop('checked', config.auto_push || false);
        $('#settingsDefaultBranch').val(config.default_branch || 'main');
        $('#settingsDefaultPriority').val(config.default_priority || 2);
        $('#settingsAutoArchive').val(config.auto_archive_days || 0);

        // Reset token input to password type
        $('#settingsGithubToken').attr('type', 'password');
        $('#btnToggleToken').text('Zeigen');

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
            showToast('Bitte Token eingeben', 'error');
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
                              '<span>Verbunden als <strong>' + escapeHtml(data.username) + '</strong></span>');
                    // Update user info
                    updateUserProfile(data);
                })
                .fail(function(xhr) {
                    $('#settingsGithubStatus')
                        .removeClass('hidden success')
                        .addClass('error')
                        .html('<span class="github-status-icon">&#10060;</span>' +
                              '<span>Token ungueltig</span>');
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
            $name.text('Nicht verbunden');
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
            showToast('GitHub Verbindung getrennt', 'success');
            $('#userProfile').removeClass('open');
        })
        .fail(function(xhr) {
            showToast('Fehler beim Trennen', 'error');
        });
    }
});
