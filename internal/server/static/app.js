(function () {
  const tabs = document.getElementById("tabs");
  const terminalPane = document.getElementById("terminal-pane");
  const terminalStrip = document.getElementById("terminal-strip");
  const emptyState = document.getElementById("empty-state");
  const heartbeat = document.getElementById("heartbeat");
  const historyOverlay = document.getElementById("history-overlay");
  const historyContent = document.getElementById("history-content");
  const historyNotice = document.getElementById("history-notice");
  const historyNewOutput = document.getElementById("history-new-output");
  const historyReflow = document.getElementById("history-reflow");
  const historyFixed = document.getElementById("history-fixed");
  const historyClose = document.getElementById("history-close");
  const historyPaste = document.getElementById("history-paste");
  const historyPastePanel = document.getElementById("history-paste-panel");
  const historyPasteStatus = document.getElementById("history-paste-status");
  const historyPasteFallback = document.getElementById("history-paste-fallback");
  const actionsMenu = document.getElementById("actions-menu");
  const actionsToggle = document.getElementById("actions-toggle");
  const actionsPopover = document.getElementById("actions-popover");
  const keysToggle = document.getElementById("keys-toggle");
  const tControlToggle = document.getElementById("tcontrol-toggle");
  const resizeToggle = document.getElementById("resize-toggle");
  const historyToggle = document.getElementById("history-toggle");
  const pasteToggle = document.getElementById("paste-toggle");
  const logoutForm = document.getElementById("logout-form");
  const scrollGestureModeControl = document.getElementById("scroll-gesture-mode");
  const versionBadge = document.getElementById("version-badge");
  const keyPanel = document.getElementById("key-panel");
  const keysClose = document.getElementById("keys-close");
  const keyGrid = document.getElementById("key-grid");
  const tControlPanel = document.getElementById("tcontrol-panel");
  const tControlClose = document.getElementById("tcontrol-close");
  const tControlWindows = document.getElementById("tcontrol-windows");
  const tControlGrid = document.getElementById("tcontrol-grid");
  const resizePanel = document.getElementById("resize-panel");
  const resizeClose = document.getElementById("resize-close");
  const resizeModes = document.getElementById("resize-modes");
  const resizeViewers = document.getElementById("resize-viewers");
  const resizePrimary = document.getElementById("resize-primary");
  const resizeStatus = document.getElementById("resize-status");
  const resizeApply = document.getElementById("resize-apply");
  const newSessionToggle = document.getElementById("new-session-toggle");
  const terminateSessionToggle = document.getElementById("terminate-session-toggle");
  const createSessionDialog = document.getElementById("create-session-dialog");
  const createSessionForm = document.getElementById("create-session-form");
  const createSessionName = document.getElementById("create-session-name");
  const createSessionStatus = document.getElementById("create-session-status");
  const createSessionCancel = document.getElementById("create-session-cancel");
  const createSessionSubmit = document.getElementById("create-session-submit");
  const terminateSessionDialog = document.getElementById("terminate-session-dialog");
  const terminateSessionForm = document.getElementById("terminate-session-form");
  const terminateSessionName = document.getElementById("terminate-session-name");
  const terminateSessionStatus = document.getElementById("terminate-session-status");
  const terminateSessionCancel = document.getElementById("terminate-session-cancel");
  const terminateSessionSubmit = document.getElementById("terminate-session-submit");
  const pasteConfirmDialog = document.getElementById("paste-confirm-dialog");
  const pasteConfirmForm = document.getElementById("paste-confirm-form");
  const pasteConfirmSummary = document.getElementById("paste-confirm-summary");
  const pasteConfirmWarning = document.getElementById("paste-confirm-warning");
  const pasteConfirmStatus = document.getElementById("paste-confirm-status");
  const pasteConfirmCancel = document.getElementById("paste-confirm-cancel");
  const pasteConfirmSubmit = document.getElementById("paste-confirm-submit");
  const lifecycleNotice = document.getElementById("lifecycle-notice");
  const frames = new Map();
  const specialKeys = [
    { key: "ctrl-c", label: "Ctrl+C", title: "Interrupt", urgent: true },
    { key: "ctrl-d", label: "Ctrl+D", title: "EOF" },
    { key: "ctrl-z", label: "Ctrl+Z", title: "Suspend" },
    { key: "ctrl-l", label: "Ctrl+L", title: "Clear screen" },
    { key: "escape", label: "Esc", title: "Escape" },
    { key: "tab", label: "Tab", title: "Tab" },
    { key: "enter", label: "Enter", title: "Enter" },
    { key: "backspace", label: "Backspace", title: "Backspace" },
    { key: "up", label: "Up", title: "Arrow up" },
    { key: "down", label: "Down", title: "Arrow down" },
    { key: "left", label: "Left", title: "Arrow left" },
    { key: "right", label: "Right", title: "Arrow right" },
    { key: "home", label: "Home", title: "Home" },
    { key: "end", label: "End", title: "End" },
    { key: "page-up", label: "PgUp", title: "Page up" },
    { key: "page-down", label: "PgDn", title: "Page down" },
    { key: "ctrl-a", label: "Ctrl+A", title: "Line start" },
    { key: "ctrl-e", label: "Ctrl+E", title: "Line end" },
    { key: "ctrl-u", label: "Ctrl+U", title: "Kill before cursor" },
    { key: "ctrl-k", label: "Ctrl+K", title: "Kill after cursor" },
    { key: "ctrl-r", label: "Ctrl+R", title: "Reverse search" },
    { key: "ctrl-w", label: "Ctrl+W", title: "Delete word" },
    { key: "delete", label: "Delete", title: "Delete" }
  ];
  const tmuxControls = [
    { action: "new-window", label: "New win", title: "New tmux window" },
    { action: "rename-window", label: "Rename", title: "Rename active tmux window", prompt: true },
    { action: "previous-window", label: "Prev win", title: "Previous tmux window" },
    { action: "next-window", label: "Next win", title: "Next tmux window" },
    { action: "choose-window", label: "Chooser", title: "Tmux window chooser" },
    { action: "command-prompt", label: ": Prompt", title: "Tmux command prompt" },
    { action: "split-horizontal", label: "Split H", title: "Split pane left/right" },
    { action: "split-vertical", label: "Split V", title: "Split pane top/bottom" },
    { action: "select-pane-left", label: "Pane L", title: "Select left pane" },
    { action: "select-pane-right", label: "Pane R", title: "Select right pane" },
    { action: "select-pane-up", label: "Pane U", title: "Select upper pane" },
    { action: "select-pane-down", label: "Pane D", title: "Select lower pane" },
    { action: "resize-pane-left", label: "Size L", title: "Resize pane left" },
    { action: "resize-pane-right", label: "Size R", title: "Resize pane right" },
    { action: "resize-pane-up", label: "Size U", title: "Resize pane up" },
    { action: "resize-pane-down", label: "Size D", title: "Resize pane down" },
    { action: "toggle-zoom", label: "Zoom", title: "Toggle pane zoom" },
    { action: "close-pane", label: "Close pane", title: "Close active tmux pane", confirm: "Close active tmux pane?", urgent: true },
    { action: "close-window", label: "Close win", title: "Close active tmux window", confirm: "Close active tmux window?", urgent: true }
  ];
  const UIState = Object.freeze({
    LIVE: "LIVE",
    HISTORY_LOADING: "HISTORY_LOADING",
    HISTORY: "HISTORY",
    COPY: "COPY",
    PASTE_PENDING: "PASTE_PENDING",
    LIVE_RECONNECTING: "LIVE_RECONNECTING"
  });
  const TransportState = Object.freeze({ CONNECTING: "CONNECTING", CONNECTED: "CONNECTED" });
  const terminalTransportMessageType = "control-agents:terminal-transport";
  let activeId = "";
  let csrfToken = "";
  let csrfTokenRequest = null;
  let renderedSessions = [];
  let sessionOrder = [];
  let sessionRefreshEpoch = 0;
  let latestSessionRefresh = 0;
  let latestSessionRefreshRecord = null;
  let createSubmitting = false;
  let terminateSubmitting = false;
  let terminateTarget = null;
  let createDialogOpener = null;
  let terminateDialogOpener = null;
  let dialogFocusOwner = null;
  let lifecycleNoticeTimer = 0;
  let tmuxWindows = [];
  let tmuxControlSubmitting = false;
  let pendingTerminalRepaintTimer = 0;
  let pendingViewportResizeTimers = [];
  let pendingStableLayoutResizeTimer = 0;
  let pendingViewerHeartbeatTimer = 0;
  let appViewportTransient = false;
  let stableLayoutMetrics = null;
  let viewerHeartbeatInFlight = false;
  let lastResizeViewerHeartbeatError = "";
  let resizeState = { mode: "fixed", selectedViewerId: "", viewers: [], window: null, capabilities: [], applied: null };
  let resizeDraftMode = "fixed";
  let resizeDraftViewerId = "";
  let resizeApplying = false;
  let uiState = UIState.LIVE;
  let history = emptyHistoryState();
  let historyMaterializePending = false;
  let historyInputInFlight = false;
  const frameTransport = new Map();
  let stagedPasteText = "";
  let pasteSubmitting = false;
  let pasteTransitioning = false;
  const frameActivityBindings = new WeakMap();
  const resizeViewerId = getResizeViewerId();
  let scrollGestureMode = getScrollGestureMode();

  function getResizeViewerId() {
    const key = "control-agents.resizeViewerId";
    try {
      const existing = window.sessionStorage.getItem(key);
      if (existing) return existing;
      const next = createId("viewer");
      window.sessionStorage.setItem(key, next);
      return next;
    } catch (error) {
      return createId("viewer");
    }
  }

  function createId(prefix) {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return `${prefix}-${window.crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

  function getScrollGestureMode() {
    const key = "control-agents.scrollGestureMode";
    try {
      return window.sessionStorage.getItem(key) === "application" ? "application" : "history";
    } catch (error) {
      return "history";
    }
  }

  function setScrollGestureMode(mode) {
    scrollGestureMode = mode === "application" ? "application" : "history";
    scrollGestureModeControl.value = scrollGestureMode;
    try {
      window.sessionStorage.setItem("control-agents.scrollGestureMode", scrollGestureMode);
    } catch (error) {
      // The tab-local preference remains active when storage is unavailable.
    }
  }

  function emptyHistoryState() {
    return {
      sessionId: "",
      paneRef: "",
      snapshotId: "",
      before: "",
      hasMore: false,
      loadingOlder: false,
      mode: "reflow",
      columns: 0,
      alternateScreen: false,
      controller: null,
      activityTimer: 0,
      activityPolling: false,
      liveFingerprint: "",
      bidiWarning: false,
      pendingWheelDelta: 0
    };
  }

  function setHeartbeat(state) {
    heartbeat.dataset.state = state;
    if (state === "online") {
      heartbeat.title = "Server connected";
      heartbeat.setAttribute("aria-label", "Server connected");
    } else if (state === "offline") {
      heartbeat.title = "Server unreachable";
      heartbeat.setAttribute("aria-label", "Server unreachable");
    } else {
      heartbeat.title = "Checking server";
      heartbeat.setAttribute("aria-label", "Checking server");
    }
  }

  async function ensureCSRFToken() {
    if (csrfToken) return csrfToken;
    if (!csrfTokenRequest) {
      csrfTokenRequest = fetch("/api/csrf", { credentials: "same-origin", cache: "no-store" })
        .then(async (response) => {
          if (response.status === 401) {
            window.location.href = "/login";
            throw new Error("authentication expired");
          }
          if (!response.ok) throw new Error(`CSRF token request failed: ${response.status}`);
          const payload = await response.json();
          if (!payload || typeof payload.token !== "string" || !payload.token) {
            throw new Error("invalid CSRF token response");
          }
          csrfToken = payload.token;
          return csrfToken;
        })
        .finally(() => {
          csrfTokenRequest = null;
        });
    }
    return csrfTokenRequest;
  }

  async function mutationFetch(resource, options) {
    const token = await ensureCSRFToken();
    const request = { ...(options || {}) };
    request.headers = new Headers(request.headers || {});
    request.headers.set("X-Control-Agents-CSRF-Token", token);
    request.credentials = "same-origin";
    return fetch(resource, request);
  }

  async function logout(event) {
    event.preventDefault();
    try {
      const response = await mutationFetch("/logout", { method: "POST" });
      window.location.href = response.redirected ? response.url : "/login";
    } catch (error) {
      console.error(error);
    }
  }

  async function fetchSessions() {
    const response = await fetch("/api/sessions", { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return [];
    }
    if (!response.ok) {
      throw new Error(`session request failed: ${response.status}`);
    }
    const payload = await response.json();
    return payload.sessions || [];
  }

  function invalidateSessionRefreshes() {
    sessionRefreshEpoch += 1;
    latestSessionRefresh += 1;
    latestSessionRefreshRecord = null;
  }

  function normalizeSessions(sessions) {
    const normalized = [];
    const seen = new Set();
    for (const session of Array.isArray(sessions) ? sessions : []) {
      if (!session || typeof session.id !== "string" || !session.id || seen.has(session.id)) continue;
      seen.add(session.id);
      normalized.push(session);
    }
    return normalized;
  }

  function mergeRenderedSession(session) {
    const sessions = [...renderedSessions];
    const existingIndex = sessions.findIndex((candidate) => candidate.id === session.id);
    if (existingIndex >= 0) {
      sessions[existingIndex] = session;
    } else {
      sessions.push(session);
    }
    return sessions;
  }

  function sessionById(id) {
    return renderedSessions.find((session) => session.id === id) || null;
  }

  function activePaneRef() {
    const session = sessionById(activeId);
    return session && typeof session.activePaneRef === "string" ? session.activePaneRef : "";
  }

  function mutationPayload(payload) {
    return { ...(payload || {}), paneRef: activePaneRef() };
  }

  function updateActivePaneRef(ref) {
    if (!activeId || typeof ref !== "string" || !ref) return;
    const session = sessionById(activeId);
    if (session) session.activePaneRef = ref;
  }

  async function fetchVersion() {
    const response = await fetch("/api/version", { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`version request failed: ${response.status}`);
    }
    return response.json();
  }

  function setVersionInfo(info) {
    if (!info || !info.version) {
      versionBadge.hidden = true;
      return;
    }

    const version = String(info.version);
    const label = version === "dev" ? "dev" : `v${version}`;
    const title = [
      `Version ${version}`,
      info.commit ? `Commit ${info.commit}` : "",
      info.buildDate ? `Built ${info.buildDate}` : ""
    ].filter(Boolean);

    versionBadge.textContent = label;
    versionBadge.title = title.join("\n");
    versionBadge.setAttribute("aria-label", title.join(", "));
    versionBadge.hidden = false;
  }

  async function readClipboardText() {
    if (!navigator.clipboard || typeof navigator.clipboard.readText !== "function") {
      throw new Error("clipboard read is unavailable");
    }
    return navigator.clipboard.readText();
  }

  async function postPaste(text) {
    if (!activeId) return;
    const details = pasteDetails(text);
    const digest = await pasteDigest(text);
    const tokenResponse = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/paste/token`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(mutationPayload({
        digest,
        bytes: details.bytes,
        lines: details.lines,
        controlCharacters: details.containsControl,
        trailingNewline: details.trailingNewline
      }))
    });
    if (tokenResponse.status === 401) {
      window.location.href = "/login";
      return;
    }
    if (!tokenResponse.ok) throw new Error(`paste token request failed: ${tokenResponse.status}`);
    const tokenPayload = await tokenResponse.json();
    if (!tokenPayload || typeof tokenPayload.token !== "string" || !tokenPayload.token) {
      throw new Error("invalid paste token response");
    }
    const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/paste`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(mutationPayload({ text, token: tokenPayload.token }))
    });
    if (response.status === 401) {
      window.location.href = "/login";
      return;
    }
    if (!response.ok) {
      throw new Error(`paste request failed: ${response.status}`);
    }
  }

  function showHistoryPasteFallback(message, focus) {
    if (!historyIsOpen()) return;
    historyPastePanel.hidden = false;
    historyPasteStatus.textContent = message || "";
    if (focus) {
      window.requestAnimationFrame(() => historyPasteFallback.focus({ preventScroll: true }));
    }
  }

  function clearStagedPaste() {
    stagedPasteText = "";
    historyPasteFallback.value = "";
    pasteConfirmSummary.textContent = "";
    pasteConfirmWarning.textContent = "";
    pasteConfirmWarning.hidden = true;
    pasteConfirmStatus.textContent = "";
    pasteConfirmSubmit.textContent = "Paste to terminal";
    pasteConfirmSubmit.disabled = false;
    pasteConfirmCancel.disabled = false;
    pasteSubmitting = false;
  }

  function utf8ByteCount(text) {
    if (window.TextEncoder) return new TextEncoder().encode(text).byteLength;
    return new Blob([text]).size;
  }

  function pasteDetails(text) {
    const lines = text.split(/\r\n|\r|\n/).length;
    const containsControl = /[\u0000-\u001f\u007f-\u009f]/.test(text);
    const trailingNewline = /(?:\r\n|\r|\n)$/.test(text);
    return { bytes: utf8ByteCount(text), lines, containsControl, trailingNewline, requiresConfirmation: lines > 1 || containsControl || trailingNewline };
  }

  async function pasteDigest(text) {
    if (!window.crypto || !window.crypto.subtle || !window.TextEncoder) {
      throw new Error("secure paste digest is unavailable");
    }
    const digest = new Uint8Array(await window.crypto.subtle.digest("SHA-256", new TextEncoder().encode(text)));
    let binary = "";
    for (const value of digest) binary += String.fromCharCode(value);
    return window.btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function stagePaste(text) {
    historyPasteFallback.value = "";
    if (!text) {
      showHistoryPasteFallback("Clipboard is empty.", true);
      return;
    }
    stagedPasteText = text;
    const details = pasteDetails(text);
    pasteConfirmSummary.textContent = `${details.bytes} UTF-8 byte${details.bytes === 1 ? "" : "s"}, ${details.lines} logical line${details.lines === 1 ? "" : "s"}.`;
    pasteConfirmWarning.hidden = !details.requiresConfirmation;
    const warnings = [];
    if (details.lines > 1) warnings.push("multiple lines");
    if (details.containsControl) warnings.push("control characters");
    if (details.trailingNewline) warnings.push("a trailing newline that may execute a command");
    pasteConfirmWarning.textContent = details.requiresConfirmation
      ? `This text contains ${warnings.join(", ")}. Confirm only if you intend to send it exactly as pasted.`
      : "";
    pasteConfirmSubmit.textContent = details.requiresConfirmation ? "Confirm paste" : "Paste to terminal";
    pasteConfirmStatus.textContent = "";
    setUIState(UIState.PASTE_PENDING);
    pasteConfirmDialog.showModal();
    dialogFocusOwner = pasteConfirmCancel;
    window.requestAnimationFrame(() => pasteConfirmCancel.focus());
  }

  async function pasteFromClipboard() {
    if (!activeId) return;
    const clipboardRead = readClipboardText();
    const historyOpening = historyIsOpen() ? Promise.resolve() : openHistory("reflow");
    showHistoryPasteFallback("Reading clipboard...", false);
    try {
      const text = await clipboardRead;
      await historyOpening;
      if (!historyIsOpen()) return;
      showHistoryPasteFallback("", false);
      stagePaste(text);
    } catch (error) {
      await historyOpening;
      if (!historyIsOpen()) return;
      showHistoryPasteFallback("Clipboard access was denied or unavailable. Use the field below.", true);
    }
  }

  function cancelPasteConfirmation() {
    if (pasteSubmitting) return;
    clearStagedPaste();
    pasteConfirmDialog.close();
  }

  async function confirmPaste(event) {
    event.preventDefault();
    if (pasteSubmitting || !stagedPasteText || !activeId) return;
    const text = stagedPasteText;
    pasteSubmitting = true;
    pasteTransitioning = true;
    pasteConfirmSubmit.disabled = true;
    pasteConfirmCancel.disabled = true;
    pasteConfirmStatus.textContent = "Sending...";
    setUIState(liveUIState());
    pasteConfirmDialog.close();
    try {
      await postPaste(text);
      clearStagedPaste();
      pasteTransitioning = false;
      closeHistory();
    } catch (error) {
      clearStagedPaste();
      pasteTransitioning = false;
      setUIState(UIState.HISTORY);
      showHistoryPasteFallback("Paste failed. Nothing was retried or sent again.", false);
      historyOverlay.focus({ preventScroll: true });
      console.error(error);
    }
  }

  function setUIState(next) {
    uiState = next;
    terminalPane.dataset.uiState = next;
    updateHistoryControls();
  }

  function liveUIState() {
    return frameTransport.get(activeId) === TransportState.CONNECTED ? UIState.LIVE : UIState.LIVE_RECONNECTING;
  }

  function setFrameTransportState(sessionId, state) {
    if (!frames.has(sessionId)) return;
    const next = state === TransportState.CONNECTED ? TransportState.CONNECTED : TransportState.CONNECTING;
    const frame = frames.get(sessionId);
    frameTransport.set(sessionId, next);
    frame.dataset.transportState = next;
    if (sessionId !== activeId) return;
    updateKeyButtons(false);
    if (historyIsOpen()) return;
    setUIState(next === TransportState.CONNECTED ? UIState.LIVE : UIState.LIVE_RECONNECTING);
  }

  function handleTerminalTransportMessage(event) {
    if (event.origin !== window.location.origin || !event.data || event.data.type !== terminalTransportMessageType) return;
    for (const [sessionId, frame] of frames.entries()) {
      if (frame.contentWindow !== event.source) continue;
      setFrameTransportState(sessionId, event.data.state);
      preserveOpenDialogFocus();
      return;
    }
  }

  function historyIsOpen() {
    return uiState === UIState.HISTORY_LOADING || uiState === UIState.HISTORY || uiState === UIState.COPY || uiState === UIState.PASTE_PENDING;
  }

  function updateHistoryControls() {
    const open = historyIsOpen();
    terminalPane.classList.toggle("history-open", open);
    historyOverlay.hidden = !open;
    historyToggle.disabled = !activeId || frames.size === 0;
    pasteToggle.disabled = uiState === UIState.PASTE_PENDING || !activeId || frames.size === 0;
    historyPaste.disabled = uiState === UIState.PASTE_PENDING || uiState === UIState.HISTORY_LOADING;
    historyToggle.classList.toggle("active", open);
    historyToggle.setAttribute("aria-pressed", String(open));
    historyReflow.classList.toggle("active", history.mode === "reflow");
    historyFixed.classList.toggle("active", history.mode === "fixed");
    historyReflow.setAttribute("aria-pressed", String(history.mode === "reflow"));
    historyFixed.setAttribute("aria-pressed", String(history.mode === "fixed"));
    historyReflow.disabled = uiState === UIState.HISTORY_LOADING || history.alternateScreen;
    historyFixed.disabled = uiState === UIState.HISTORY_LOADING;
    for (const [id, frame] of frames.entries()) {
      const inert = open && id === activeId;
      if (inert) {
        frame.setAttribute("inert", "");
      } else {
        frame.removeAttribute("inert");
      }
      if ("inert" in frame) frame.inert = inert;
    }
  }

  async function openHistory(requestedMode, initialWheelDelta) {
    if (!activeId || !activePaneRef()) return;
    const sessionId = activeId;
    const paneRef = activePaneRef();
    const mode = requestedMode === "fixed" ? "fixed" : "reflow";
    const previousSnapshotId = history.snapshotId;
    closeHistory({ deleteSnapshot: false, restoreFocus: false });
    const controller = new AbortController();
    history = {
      ...emptyHistoryState(),
      sessionId,
      paneRef,
      mode,
      controller,
      pendingWheelDelta: Number.isFinite(initialWheelDelta) ? initialWheelDelta : 0
    };
    setKeyPanelOpen(false);
    setTControlPanelOpen(false);
    setResizePanelOpen(false);
    historyContent.replaceChildren(historyMessage("Loading terminal history..."));
    historyPastePanel.hidden = true;
    historyPasteStatus.textContent = "";
    historyPasteFallback.value = "";
    historyNotice.hidden = true;
    historyNewOutput.hidden = true;
    setUIState(UIState.HISTORY_LOADING);
    historyOverlay.focus({ preventScroll: true });
    try {
      await releaseHistorySnapshot(previousSnapshotId);
      if (history.controller !== controller || controller.signal.aborted) return;
      const response = await mutationFetch(`/api/v1/panes/${encodeURIComponent(paneRef)}/history-snapshots`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Control-Agents-Viewer-ID": resizeViewerId },
        credentials: "same-origin",
        signal: controller.signal,
        body: JSON.stringify({ mode })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) throw new Error(`history snapshot failed: ${response.status}`);
      const page = await response.json();
      if (history.controller !== controller || activeId !== sessionId) {
        releaseHistorySnapshot(page.snapshotId);
        return;
      }
      applyHistoryPage(page, false);
      history.liveFingerprint = terminalFingerprint(frames.get(sessionId));
      setUIState(UIState.HISTORY);
      historyOverlay.scrollTop = historyOverlay.scrollHeight;
      applyPendingHistoryWheel();
      if (page.followedByOutput) markHistoryNewOutput(sessionId);
      if (historyNewOutput.hidden) startHistoryActivityPoll();
      historyOverlay.focus({ preventScroll: true });
    } catch (error) {
      if (error && error.name === "AbortError") return;
      if (history.controller !== controller) return;
      historyContent.replaceChildren(historyMessage("Failed to load terminal history."));
      setUIState(UIState.HISTORY);
      console.error(error);
    }
  }

  function closeHistory(options) {
    const settings = options || {};
    const previous = history;
    if (previous.controller) previous.controller.abort();
    if (previous.activityTimer) window.clearInterval(previous.activityTimer);
    if (settings.deleteSnapshot !== false && previous.snapshotId) {
      releaseHistorySnapshot(previous.snapshotId);
    }
    history = emptyHistoryState();
    historyMaterializePending = false;
    historyContent.replaceChildren();
    historyPastePanel.hidden = true;
    historyPasteStatus.textContent = "";
    historyPasteFallback.value = "";
    historyNotice.hidden = true;
    historyNewOutput.hidden = true;
    if (pasteConfirmDialog.open) pasteConfirmDialog.close();
    clearStagedPaste();
    pasteTransitioning = false;
    setUIState(liveUIState());
    if (settings.restoreFocus !== false) scheduleLiveTerminalRepaint();
  }

  function releaseHistorySnapshot(snapshotId) {
    if (!snapshotId) return Promise.resolve();
    return mutationFetch(`/api/v1/history-snapshots/${encodeURIComponent(snapshotId)}`, {
      method: "DELETE",
      headers: { "X-Control-Agents-Viewer-ID": resizeViewerId },
      credentials: "same-origin",
      keepalive: true
    }).catch(() => undefined);
  }

  function historyMessage(message) {
    const line = document.createElement("div");
    line.className = "history-line history-message";
    line.textContent = message;
    return line;
  }

  function applyHistoryPage(page, prepend) {
    if (!page || typeof page.snapshotId !== "string" || !Array.isArray(page.lines)) {
      throw new Error("invalid history page");
    }
    history.snapshotId = page.snapshotId;
    history.before = typeof page.before === "string" ? page.before : "";
    history.hasMore = Boolean(page.hasMore && history.before);
    history.mode = page.mode === "fixed" ? "fixed" : "reflow";
    history.columns = Number.isFinite(Number(page.columns)) ? Math.max(1, Number(page.columns)) : 80;
    history.alternateScreen = Boolean(page.alternateScreen);
	    history.bidiWarning = history.bidiWarning || page.lines.some((line) => line && line.bidiWarning === true);
    historyContent.classList.toggle("fixed", history.mode === "fixed");
    historyContent.classList.toggle("reflow", history.mode !== "fixed");
    historyContent.style.setProperty("--history-columns", String(history.columns));
    updateHistoryNotice();
    const fragment = document.createDocumentFragment();
    for (const line of page.lines) fragment.appendChild(renderHistoryLine(line));
    if (prepend) {
      historyContent.prepend(fragment);
    } else {
      historyContent.replaceChildren(fragment);
    }
  }

  function renderHistoryLine(line) {
    const element = document.createElement("div");
    element.className = "history-line";
    if (line && line.bidiWarning === true) {
      element.classList.add("bidi-warning");
      element.title = "Bidirectional text controls were replaced with visible markers for safe copying.";
    }
    const runs = line && Array.isArray(line.runs) ? line.runs : [];
    for (const run of runs) {
      const span = document.createElement("span");
      span.className = "history-run";
      const style = run && run.style && typeof run.style === "object" ? run.style : {};
      for (const name of ["bold", "faint", "italic", "underline", "strike", "inverse"]) {
        if (style[name] === true) span.classList.add(name);
      }
      const foreground = safeHistoryColor(style.foreground);
      const background = safeHistoryColor(style.background);
      if (style.inverse === true) {
        span.style.color = background || "var(--background)";
        span.style.backgroundColor = foreground || "var(--text)";
      } else {
        if (foreground) span.style.color = foreground;
        if (background) span.style.backgroundColor = background;
      }
      span.appendChild(document.createTextNode(run && typeof run.text === "string" ? run.text : ""));
      element.appendChild(span);
    }
    return element;
  }

  function updateHistoryNotice() {
    const notices = [];
    if (history.alternateScreen) {
      notices.push("Alternate-screen applications redraw in place, so History can preserve only the captured fixed grid and available tmux history.");
    }
    if (history.bidiWarning) {
      notices.push("Bidirectional text controls were replaced with visible [BIDI U+....] markers so copied commands cannot hide their display order.");
    }
    historyNotice.textContent = notices.join(" ");
    historyNotice.hidden = notices.length === 0;
  }

  function safeHistoryColor(value) {
    return typeof value === "string" && /^#[0-9a-f]{6}$/i.test(value) ? value : "";
  }

  function localWheelDelta(event) {
    const pageHeight = Math.max(160, historyOverlay.clientHeight || window.innerHeight || 600);
    const multiplier = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16
      : event.deltaMode === WheelEvent.DOM_DELTA_PAGE ? pageHeight
        : 1;
    return Math.max(-pageHeight, Math.min(pageHeight, event.deltaY * multiplier));
  }

  function routeWheelToHistory(event) {
    const delta = localWheelDelta(event);
    if (!delta) return;
    if (uiState === UIState.HISTORY_LOADING) {
      const limit = Math.max(640, (historyOverlay.clientHeight || window.innerHeight || 600) * 4);
      history.pendingWheelDelta = Math.max(-limit, Math.min(limit, history.pendingWheelDelta + delta));
      return;
    }
    historyOverlay.scrollTop += delta;
  }

  function applyPendingHistoryWheel() {
    const snapshotId = history.snapshotId;
    const delta = history.pendingWheelDelta;
    history.pendingWheelDelta = 0;
    if (!delta) return;
    window.requestAnimationFrame(() => {
      if (history.snapshotId === snapshotId && historyIsOpen()) historyOverlay.scrollTop += delta;
    });
  }

  function historySelectionActive() {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || selection.rangeCount === 0) return false;
    if (historyOverlay.contains(selection.anchorNode) || historyOverlay.contains(selection.focusNode)) return true;
    for (let index = 0; index < selection.rangeCount; index += 1) {
      try {
        if (selection.getRangeAt(index).intersectsNode(historyOverlay)) return true;
      } catch (error) {
        // A range can become stale during native selection teardown.
      }
    }
    return false;
  }

  async function loadOlderHistory() {
    if (!history.snapshotId || !history.hasMore || history.loadingOlder || uiState === UIState.HISTORY_LOADING) return;
    if (historySelectionActive()) {
      historyMaterializePending = true;
      return;
    }
    history.loadingOlder = true;
    const snapshotId = history.snapshotId;
    const cursor = history.before;
    const anchor = Array.from(historyContent.children).find((line) => line.getBoundingClientRect().bottom >= historyOverlay.getBoundingClientRect().top) || historyContent.firstElementChild;
    const anchorTop = anchor ? anchor.getBoundingClientRect().top : 0;
    try {
      const response = await fetch(`/api/v1/history-snapshots/${encodeURIComponent(snapshotId)}/pages?before=${encodeURIComponent(cursor)}`, {
        headers: { "X-Control-Agents-Viewer-ID": resizeViewerId },
        credentials: "same-origin"
      });
      if (!response.ok) throw new Error(`history page failed: ${response.status}`);
      const page = await response.json();
      if (history.snapshotId !== snapshotId || history.before !== cursor) return;
      if (historySelectionActive()) {
        historyMaterializePending = true;
        return;
      }
      applyHistoryPage(page, true);
      if (anchor) {
        const delta = anchor.getBoundingClientRect().top - anchorTop;
        historyOverlay.scrollTop += delta;
      }
    } catch (error) {
      console.error(error);
    } finally {
      if (history.snapshotId === snapshotId) history.loadingOlder = false;
    }
  }

  function markHistoryNewOutput(sessionId) {
    if (!historyIsOpen() || history.sessionId !== sessionId || uiState === UIState.HISTORY_LOADING) return;
    if (history.activityTimer) {
      window.clearInterval(history.activityTimer);
      history.activityTimer = 0;
    }
    historyNewOutput.hidden = false;
  }

  function startHistoryActivityPoll() {
    if (!history.snapshotId || history.activityTimer) return;
    const snapshotId = history.snapshotId;
    history.activityTimer = window.setInterval(() => {
      if (!checkHistoryLiveFrame(history.sessionId, frames.get(history.sessionId))) {
        pollHistoryActivity(snapshotId);
      }
    }, 750);
  }

  function checkHistoryLiveFrame(sessionId, frame) {
    if (!historyIsOpen() || history.sessionId !== sessionId || uiState === UIState.HISTORY_LOADING) return false;
    const fingerprint = terminalFingerprint(frame);
    if (!fingerprint) return false;
    if (!history.liveFingerprint) {
      history.liveFingerprint = fingerprint;
      return false;
    }
    if (fingerprint === history.liveFingerprint) return false;
    markHistoryNewOutput(sessionId);
    return true;
  }

  function terminalFingerprint(frame) {
    if (!frame) return "";
    try {
      const win = frame.contentWindow;
      const terminal = win && (win.term || win.terminal || win.xterm);
      const buffer = terminal && terminal.buffer && terminal.buffer.active;
      if (!buffer || typeof buffer.getLine !== "function") return "";
      let hash = 2166136261;
      const mix = (value) => {
        const text = String(value);
        for (let index = 0; index < text.length; index += 1) {
          hash ^= text.charCodeAt(index);
          hash = Math.imul(hash, 16777619);
        }
      };
      mix(`${buffer.length}:${buffer.baseY}:${buffer.cursorX}:${buffer.cursorY}`);
      const rows = Math.max(1, Number(terminal.rows) || 40);
      for (let index = Math.max(0, buffer.length - rows); index < buffer.length; index += 1) {
        const line = buffer.getLine(index);
        if (line && typeof line.translateToString === "function") mix(line.translateToString(false));
        mix("\n");
      }
      return `${buffer.length}:${hash >>> 0}`;
    } catch (error) {
      return "";
    }
  }

  async function pollHistoryActivity(snapshotId) {
    if (history.snapshotId !== snapshotId || history.activityPolling || !historyNewOutput.hidden) return;
    history.activityPolling = true;
    try {
      const response = await fetch(`/api/v1/history-snapshots/${encodeURIComponent(snapshotId)}`, {
        headers: { "X-Control-Agents-Viewer-ID": resizeViewerId },
        credentials: "same-origin",
        signal: history.controller ? history.controller.signal : undefined
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) return;
      const activity = await response.json();
      if (history.snapshotId === snapshotId && activity && activity.newOutput === true) {
        markHistoryNewOutput(history.sessionId);
      }
    } catch (error) {
      if (!error || error.name !== "AbortError") console.error(error);
    } finally {
      if (history.snapshotId === snapshotId) history.activityPolling = false;
    }
  }

  async function refreshVersion() {
    try {
      setVersionInfo(await fetchVersion());
    } catch (error) {
      versionBadge.hidden = true;
      console.error(error);
    }
  }

  function activate(id, sessionChanged) {
    if (id && !frames.has(id)) return;
    const changed = Boolean(sessionChanged) || activeId !== id;
    if (changed) {
      closeHistory({ restoreFocus: false });
    }
    activeId = id;
    for (const button of tabs.querySelectorAll("button")) {
      button.classList.toggle("active", button.dataset.sessionId === id);
    }
    for (const [frameId, frame] of frames.entries()) {
      frame.hidden = frameId !== id;
    }
    emptyState.hidden = frames.size !== 0;
    requestTerminalResize();
    updateKeyButtons(false);
    updateControlButtons();
    if (!historyIsOpen()) setUIState(liveUIState());
    updateLifecycleControls();
    if (!tControlPanel.hidden) {
      refreshTmuxControl();
    }
    if (changed && !resizePanel.hidden) {
      postResizeViewerHeartbeat().finally(refreshResizeSettings);
    } else if (changed) {
      scheduleResizeViewerHeartbeat(100);
    }
  }

  function render(sessions, preferredActiveId) {
    sessions = normalizeSessions(sessions);
    const previousActiveId = activeId;
    const previousOrder = sessionOrder;
    const nextIds = new Set(sessions.map((session) => session.id));
    for (const [id, frame] of frames.entries()) {
      if (!nextIds.has(id)) {
        unbindFrameActivity(frame);
        frame.remove();
        frames.delete(id);
        frameTransport.delete(id);
      }
    }

    tabs.replaceChildren();
    for (const session of sessions) {
      const button = document.createElement("button");
      button.type = "button";
      button.dataset.sessionId = session.id;
      button.title = session.cwd || session.name || session.id;
      renderSessionTabContent(button, session.name || session.id, session.tmuxWindowCount || 0);
      button.addEventListener("click", () => activate(session.id));
      tabs.appendChild(button);

      if (!frames.has(session.id)) {
        const frame = document.createElement("iframe");
        frame.className = "terminal-frame";
        frame.title = session.name || session.id;
        frame.hidden = true;
        frameTransport.set(session.id, TransportState.CONNECTING);
        frame.dataset.transportState = TransportState.CONNECTING;
        frame.addEventListener("load", () => {
          bindFrameActivity(session.id, frame);
          if (session.id === activeId) {
            updateKeyButtons(false);
            scheduleResizeViewerHeartbeat(100);
          }
          preserveOpenDialogFocus();
        });
        frames.set(session.id, frame);
        terminalStrip.appendChild(frame);
        frame.src = `/terminal/${encodeURIComponent(session.id)}/`;
      }
    }

    const nextOrder = sessions.map((session) => session.id);
    if (preferredActiveId && nextIds.has(preferredActiveId)) {
      activeId = preferredActiveId;
    } else if (!activeId || !nextIds.has(activeId)) {
      const previousIndex = previousOrder.indexOf(previousActiveId);
      const fallbackIndex = previousIndex >= 0 ? Math.min(previousIndex, nextOrder.length - 1) : 0;
      activeId = fallbackIndex >= 0 && nextOrder.length ? nextOrder[fallbackIndex] : "";
    }
    renderedSessions = sessions;
    sessionOrder = nextOrder;
    if (activeId) {
      activate(activeId, activeId !== previousActiveId);
    } else {
      closeHistory({ restoreFocus: false });
      emptyState.hidden = false;
      updateKeyButtons(false);
      updateControlButtons();
      if (!resizePanel.hidden) {
        refreshResizeSettings();
      }
      updateLifecycleControls();
    }
  }

  function renderSessionTabContent(button, labelText, tmuxWindowCount) {
    button.replaceChildren();
    const label = document.createElement("span");
    label.className = "tab-label";
    label.textContent = labelText;
    button.appendChild(label);

    if (Number(tmuxWindowCount) > 1) {
      const badge = document.createElement("span");
      badge.className = "tab-window-badge";
      badge.textContent = String(tmuxWindowCount);
      badge.title = `${tmuxWindowCount} tmux windows`;
      button.appendChild(badge);
    }
  }

  function updateSessionTabBadge(sessionId, tmuxWindowCount) {
    const button = tabs.querySelector(`button[data-session-id="${sessionId}"]`);
    if (!button) return;
    const label = button.querySelector(".tab-label");
    renderSessionTabContent(button, label ? label.textContent : sessionId, tmuxWindowCount);
  }

  function refresh() {
    const record = {
      requestID: ++latestSessionRefresh,
      epoch: sessionRefreshEpoch,
      promise: null
    };
    record.promise = performSessionRefresh(record);
    latestSessionRefreshRecord = record;
    return record.promise;
  }

  async function performSessionRefresh(record) {
    try {
      const sessions = await fetchSessions();
      if (!sessionRefreshIsLatest(record)) return sessionRefreshResult("superseded", record);
      render(sessions);
      setHeartbeat("online");
      return sessionRefreshResult("applied", record);
    } catch (error) {
      if (!sessionRefreshIsLatest(record)) return sessionRefreshResult("superseded", record);
      setHeartbeat("offline");
      console.error(error);
      return sessionRefreshResult("failed", record);
    }
  }

  function sessionRefreshIsLatest(record) {
    return record.epoch === sessionRefreshEpoch && record.requestID === latestSessionRefresh;
  }

  function sessionRefreshResult(outcome, record) {
    return { outcome, requestID: record.requestID, epoch: record.epoch };
  }

  async function waitForCreatedSessionReconciliation(initialRefresh, epoch) {
    const deadline = Date.now() + 8000;
    let pendingRefresh = initialRefresh;
    while (pendingRefresh) {
      const result = await waitForSessionRefreshBefore(pendingRefresh, deadline);
      if (!result || result.epoch !== epoch) return false;
      if (result.outcome === "applied") return true;
      if (result.outcome === "failed") return false;

      const latest = latestSessionRefreshRecord;
      if (!latest || latest.epoch !== epoch || latest.requestID <= result.requestID) return false;
      pendingRefresh = latest.promise;
    }
    return false;
  }

  async function waitForSessionRefreshBefore(pendingRefresh, deadline) {
    const remaining = deadline - Date.now();
    if (remaining <= 0) return null;
    let timer = 0;
    try {
      return await Promise.race([
        pendingRefresh,
        new Promise((resolve) => {
          timer = window.setTimeout(() => resolve(null), remaining);
        })
      ]);
    } finally {
      window.clearTimeout(timer);
    }
  }

  function updateLifecycleControls() {
    terminateSessionToggle.disabled = !activeId || !sessionById(activeId);
  }

  function validateSessionName(name) {
    if (!name) return "Enter a session name.";
    if (name.length > 64) return "Session names may contain at most 64 characters.";
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name)) {
      return "Start with a letter or digit and use only letters, digits, dot, underscore, or hyphen.";
    }
    return "";
  }

  function setCreateStatus(message) {
    createSessionStatus.textContent = message || "";
  }

  function setCreateSubmitting(submitting) {
    createSubmitting = submitting;
    createSessionName.disabled = submitting;
    createSessionCancel.disabled = submitting;
    createSessionSubmit.disabled = submitting;
    createSessionSubmit.textContent = submitting ? "Creating..." : "Create";
  }

  function openCreateSessionDialog() {
    createDialogOpener = document.activeElement;
    createSessionForm.reset();
    setCreateStatus("");
    setCreateSubmitting(false);
    setActionsMenuOpen(false);
    createSessionDialog.showModal();
    dialogFocusOwner = createSessionName;
    window.requestAnimationFrame(() => createSessionName.focus());
  }

  function closeCreateSessionDialog() {
    if (createSubmitting) return;
    createSessionDialog.close();
  }

  async function createManagedSession(event) {
    event.preventDefault();
    if (createSubmitting) return;
    const name = createSessionName.value;
    const validationError = validateSessionName(name);
    if (validationError) {
      setCreateStatus(validationError);
      createSessionName.focus();
      return;
    }

    setCreateStatus("");
    setCreateSubmitting(true);
    try {
      const response = await mutationFetch("/api/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ name })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        setCreateStatus(await createSessionErrorMessage(response));
        return;
      }
      const payload = await response.json();
      const session = payload && payload.session;
      if (!session || typeof session.id !== "string" || !session.id || session.name !== name) {
        throw new Error("invalid session create response");
      }

      invalidateSessionRefreshes();
      render(mergeRenderedSession(session), session.id);
      setHeartbeat("online");
      const reconciliation = refresh();
      if (payload.created === true) {
        // A newly created session changes the web session-limit state. Keep
        // this dialog as the lifecycle owner until the latest list refresh is
        // applied so another create cannot race that transition.
        const reconciled = await waitForCreatedSessionReconciliation(reconciliation, sessionRefreshEpoch);
        if (!reconciled) {
          setCreateStatus("The session was created, but the latest session list could not be confirmed. Check the connection, then cancel or submit again.");
          return;
        }
      }
      setCreateSubmitting(false);
      createSessionDialog.close();
    } catch (error) {
      setCreateStatus("Could not reach the server. Check the connection and try again.");
      console.error(error);
    } finally {
      if (createSubmitting) setCreateSubmitting(false);
    }
  }

  async function createSessionErrorMessage(response) {
    if (response.status === 400) return "Use a valid canonical session name and try again.";
    if (response.status === 409) {
      const message = (await response.text()).trim();
      if (message === "managed session limit reached") {
        return "The managed session limit has been reached. Terminate another session before creating one.";
      }
      return "That name conflicts with an unmanaged tmux session. Choose a different name.";
    }
    if (response.status === 502 || response.status === 503) {
      return "The local session service is unavailable. Check tmux and ttyd, then try again.";
    }
    return "The session could not be created. Try again.";
  }

  function setTerminateStatus(message) {
    terminateSessionStatus.textContent = message || "";
  }

  function setTerminateSubmitting(submitting) {
    terminateSubmitting = submitting;
    terminateSessionCancel.disabled = submitting;
    terminateSessionSubmit.disabled = submitting;
    terminateSessionSubmit.textContent = submitting ? "Terminating..." : "Terminate";
  }

  function openTerminateSessionDialog() {
    const session = sessionById(activeId);
    if (!session) return;
    terminateDialogOpener = document.activeElement;
    terminateTarget = { id: session.id, name: session.name || session.id, paneRef: session.activePaneRef };
    terminateSessionName.textContent = terminateTarget.name;
    setTerminateStatus("");
    setTerminateSubmitting(false);
    setActionsMenuOpen(false);
    terminateSessionDialog.showModal();
    dialogFocusOwner = terminateSessionCancel;
    window.requestAnimationFrame(() => terminateSessionCancel.focus());
  }

  function closeTerminateSessionDialog() {
    if (terminateSubmitting) return;
    terminateSessionDialog.close();
  }

  async function terminateManagedSession(event) {
    event.preventDefault();
    if (terminateSubmitting || !terminateTarget) return;
    const target = { ...terminateTarget };
    setTerminateStatus("");
    setTerminateSubmitting(true);
    try {
      const response = await mutationFetch(`/api/sessions/${encodeURIComponent(target.id)}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ confirmName: target.name, paneRef: target.paneRef })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (response.status === 204 || response.status === 404) {
        invalidateSessionRefreshes();
        render(renderedSessions.filter((session) => session.id !== target.id));
        setTerminateSubmitting(false);
        terminateSessionDialog.close();
        if (response.status === 404) {
          showLifecycleNotice(`${target.name} was already terminated.`);
        }
        refresh();
        return;
      }
      setTerminateStatus(terminateSessionErrorMessage(response.status));
      invalidateSessionRefreshes();
      refresh();
    } catch (error) {
      setTerminateStatus("Could not reach the server. The session was not confirmed as terminated.");
      console.error(error);
    } finally {
      if (terminateSubmitting) setTerminateSubmitting(false);
    }
  }

  function terminateSessionErrorMessage(status) {
    if (status === 400) return "Confirmation was rejected. The session was not terminated.";
    if (status === 502 || status === 503) {
      return "The session lifecycle failed. Check the session state and try again.";
    }
    return "The session could not be terminated. Try again.";
  }

  function showLifecycleNotice(message) {
    lifecycleNotice.textContent = message;
    lifecycleNotice.hidden = false;
    window.clearTimeout(lifecycleNoticeTimer);
    lifecycleNoticeTimer = window.setTimeout(() => {
      lifecycleNotice.hidden = true;
      lifecycleNotice.textContent = "";
    }, 3500);
  }

  function restoreDialogFocus(opener) {
    const target = isVisibleFocusableControl(opener) ? opener : actionsToggle;
    target.focus();
    if (document.activeElement !== target && target !== actionsToggle) {
      actionsToggle.focus();
    }
  }

  function preserveOpenDialogFocus() {
    const dialog = [createSessionDialog, terminateSessionDialog, pasteConfirmDialog]
      .find((candidate) => candidate.open);
    if (!dialog) return false;
    if (dialog.contains(document.activeElement)) {
      dialogFocusOwner = document.activeElement;
      return true;
    }
    let target = dialogFocusOwner;
    if (!isVisibleFocusableControl(target) || !dialog.contains(target)) {
      target = dialog.querySelector("input:not(:disabled), textarea:not(:disabled), button:not(:disabled), select:not(:disabled)") || dialog;
    }
    const restore = () => {
      if (!dialog.open || dialog.contains(document.activeElement)) return;
      target.focus({ preventScroll: true });
    };
    restore();
    window.requestAnimationFrame(restore);
    return true;
  }

  function isVisibleFocusableControl(element) {
    if (!(element instanceof HTMLElement) || !element.isConnected || element.matches(":disabled")) return false;
    if (element.closest("[hidden]")) return false;
    const style = window.getComputedStyle(element);
    return style.display !== "none" && style.visibility !== "hidden" && element.getClientRects().length > 0;
  }

  function requestTerminalResize() {
    const frame = frames.get(activeId);
    if (!frame) return;
    window.requestAnimationFrame(() => {
      frame.style.width = "calc(100% - 1px)";
      frame.style.height = "calc(100% - 1px)";
      window.requestAnimationFrame(() => {
        frame.style.width = "";
        frame.style.height = "";
        try {
          frame.contentWindow.dispatchEvent(new Event("resize"));
        } catch (error) {
          // Cross-origin protections should not apply here, but resizing the
          // iframe element itself is enough if dispatching is unavailable.
        }
      });
    });
  }

  function updateAppViewportMetrics() {
    const viewport = window.visualViewport;
    const height = viewport && viewport.height > 0 ? viewport.height : window.innerHeight;
    const width = viewport && viewport.width > 0 ? viewport.width : window.innerWidth;
    const offsetTop = viewport && viewport.offsetTop > 0 ? viewport.offsetTop : 0;
    const offsetLeft = viewport && Number.isFinite(viewport.offsetLeft) ? Math.max(0, viewport.offsetLeft) : 0;
    if (height > 0) {
      document.documentElement.style.setProperty("--app-viewport-height", `${Math.round(height)}px`);
    }
    if (width > 0) {
      document.documentElement.style.setProperty("--app-viewport-width", `${Math.round(width)}px`);
    }
    document.documentElement.style.setProperty("--app-viewport-offset-top", `${Math.round(offsetTop)}px`);
    document.documentElement.style.setProperty("--app-viewport-offset-left", `${Math.round(offsetLeft)}px`);
    const layoutHeight = Math.max(
      height || 0,
      window.innerHeight || 0,
      document.documentElement.clientHeight || 0
    );
    const keyboardBottomOffset = Math.max(0, Math.round(layoutHeight - height - offsetTop));
    appViewportTransient = Boolean(viewport) && keyboardBottomOffset > 80;
    document.documentElement.style.setProperty("--keyboard-bottom-offset", `${keyboardBottomOffset}px`);
  }

  function readStableLayoutMetrics() {
    const orientation = window.screen && window.screen.orientation
      ? `${window.screen.orientation.type || ""}:${window.screen.orientation.angle || 0}`
      : String(window.orientation || "");
    return {
      width: Math.round(window.innerWidth || document.documentElement.clientWidth || 0),
      height: Math.round(window.innerHeight || document.documentElement.clientHeight || 0),
      orientation
    };
  }

  function sameLayoutMetrics(left, right) {
    return Boolean(left && right) && left.width === right.width && left.height === right.height && left.orientation === right.orientation;
  }

  function scheduleStableLayoutResize() {
    window.clearTimeout(pendingStableLayoutResizeTimer);
    pendingStableLayoutResizeTimer = window.setTimeout(() => {
      updateAppViewportMetrics();
      if (appViewportTransient) return;
      const next = readStableLayoutMetrics();
      if (sameLayoutMetrics(next, stableLayoutMetrics)) return;
      stableLayoutMetrics = next;
      requestTerminalResize();
      scheduleResizeViewerHeartbeat(250);
    }, 320);
  }

  function handleAppViewportChange(layoutChanged) {
    updateAppViewportMetrics();
    scheduleResizeViewerHeartbeat(450);
    if (layoutChanged) scheduleStableLayoutResize();
    for (const timer of pendingViewportResizeTimers) {
      window.clearTimeout(timer);
    }
    pendingViewportResizeTimers = [80, 180, 360].map((delay) => {
      return window.setTimeout(() => {
        updateAppViewportMetrics();
        scheduleResizeViewerHeartbeat(450);
      }, delay);
    });
  }

  function installAppViewportTracking() {
    updateAppViewportMetrics();
    stableLayoutMetrics = readStableLayoutMetrics();
    window.addEventListener("resize", () => handleAppViewportChange(true));
    window.addEventListener("orientationchange", () => handleAppViewportChange(true));
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", () => handleAppViewportChange(false));
      window.visualViewport.addEventListener("scroll", () => handleAppViewportChange(false));
    }
  }

  function scheduleLiveTerminalRepaint() {
    const frame = frames.get(activeId);
    try {
      const win = frame && frame.contentWindow;
      const terminal = win && (win.term || win.terminal || win.xterm);
      if (terminal && typeof terminal.scrollToBottom === "function") terminal.scrollToBottom();
    } catch (error) {
      // The terminal may be reconnecting; resize and focus remain best effort.
    }
    focusActiveTerminal();
    requestTerminalResize();
    window.clearTimeout(pendingTerminalRepaintTimer);
    pendingTerminalRepaintTimer = window.setTimeout(requestTerminalResize, 120);
  }

  function unbindFrameActivity(frame) {
    const binding = frameActivityBindings.get(frame);
    if (!binding) return;
    if (binding.observer) binding.observer.disconnect();
    if (binding.outputDisposable && typeof binding.outputDisposable.dispose === "function") binding.outputDisposable.dispose();
    binding.win.removeEventListener("wheel", binding.wheel, { capture: true });
    binding.win.removeEventListener("keydown", binding.keyDown, { capture: true });
    binding.win.removeEventListener("pagehide", binding.transportDisconnect);
    binding.win.removeEventListener("beforeunload", binding.transportDisconnect);
    frameActivityBindings.delete(frame);
  }

  function bindFrameActivity(sessionId, frame) {
    unbindFrameActivity(frame);
    let win;
    let doc;
    try {
      win = frame.contentWindow;
      doc = frame.contentDocument;
    } catch (error) {
      return;
    }
    if (!win || !doc) return;

    const rows = doc.querySelector(".xterm-rows");
    const observer = rows ? new MutationObserver(() => checkHistoryLiveFrame(sessionId, frame)) : null;
    if (observer) {
      observer.observe(rows, { childList: true, characterData: true, subtree: true });
    }
    let outputDisposable = null;
    for (const terminal of [win.term, win.terminal, win.xterm]) {
      if (terminal && typeof terminal.onWriteParsed === "function") {
        outputDisposable = terminal.onWriteParsed(() => checkHistoryLiveFrame(sessionId, frame));
        break;
      }
      if (terminal && typeof terminal.onRender === "function") {
        outputDisposable = terminal.onRender(() => checkHistoryLiveFrame(sessionId, frame));
        break;
      }
    }

    const wheel = (event) => {
      if (sessionId !== activeId || event.ctrlKey) return;
      if (historyIsOpen()) {
        if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
        event.preventDefault();
        event.stopPropagation();
        if (typeof event.stopImmediatePropagation === "function") event.stopImmediatePropagation();
        routeWheelToHistory(event);
        return;
      }
      if (scrollGestureMode !== "history") return;
      if (event.deltaY >= 0 || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
      event.preventDefault();
      event.stopPropagation();
      if (typeof event.stopImmediatePropagation === "function") event.stopImmediatePropagation();
      openHistory("reflow", localWheelDelta(event));
    };
    const keyDown = (event) => {
      if (sessionId !== activeId || scrollGestureMode !== "history" || event.key !== "PageUp" || event.ctrlKey || event.metaKey || event.altKey) return;
      event.preventDefault();
      event.stopPropagation();
      if (typeof event.stopImmediatePropagation === "function") event.stopImmediatePropagation();
      if (historyIsOpen()) {
        historyOverlay.scrollTop -= Math.max(160, historyOverlay.clientHeight * 0.9);
      } else {
        openHistory("reflow");
      }
    };
    const transportDisconnect = () => {
      setFrameTransportState(sessionId, TransportState.CONNECTING);
    };

    frameActivityBindings.set(frame, { win, observer, outputDisposable, wheel, keyDown, transportDisconnect });
    win.addEventListener("wheel", wheel, { capture: true, passive: false });
    win.addEventListener("keydown", keyDown, { capture: true });
    win.addEventListener("pagehide", transportDisconnect);
    win.addEventListener("beforeunload", transportDisconnect);
  }

  async function postKey(key) {
    if (!activeId || frameTransport.get(activeId) !== TransportState.CONNECTED) return false;
    updateKeyButtons(true);
    try {
      const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/keys`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(mutationPayload({ key }))
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`key request failed: ${response.status}`);
      }
      focusActiveTerminal();
      return true;
    } catch (error) {
      console.error(error);
      return false;
    } finally {
      updateKeyButtons(false);
    }
  }

  async function postText(text) {
    if (!activeId || frameTransport.get(activeId) !== TransportState.CONNECTED) return false;
    try {
      const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/keys`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(mutationPayload({ text }))
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) throw new Error(`text input request failed: ${response.status}`);
      focusActiveTerminal();
      return true;
    } catch (error) {
      console.error(error);
      return false;
    }
  }

  async function sendFirstHistoryInput(key) {
    if (historyInputInFlight) return;
    historyInputInFlight = true;
    const connected = frameTransport.get(activeId) === TransportState.CONNECTED;
    closeHistory({ restoreFocus: false });
    if (!connected) {
      setUIState(UIState.LIVE_RECONNECTING);
      historyInputInFlight = false;
      return;
    }
    try {
      if (key === "Enter") {
        await postKey("enter");
      } else if (key === "Backspace") {
        await postKey("backspace");
      } else {
        await postText(key);
      }
    } finally {
      historyInputInFlight = false;
    }
  }

  async function fetchTmuxControl() {
    if (!activeId) return { windows: [] };
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/tmux-control`, { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return { windows: [] };
    }
    if (!response.ok) {
      throw new Error(`tmux control state failed: ${response.status}`);
    }
    return response.json();
  }

  async function postTmuxControl(action, payload) {
    if (!activeId || tmuxControlSubmitting) return;
    tmuxControlSubmitting = true;
    updateControlButtons();
    try {
      const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/tmux-control`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(mutationPayload({ action, ...(payload || {}) }))
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`tmux control request failed: ${response.status}`);
      }
      const result = await response.json();
      updateActivePaneRef(result.activePaneRef);
      renderTmuxWindows(result.windows || []);
      focusActiveTerminal();
      requestTerminalResize();
    } catch (error) {
      console.error(error);
    } finally {
      tmuxControlSubmitting = false;
      updateControlButtons();
    }
  }

  async function refreshTmuxControl() {
    try {
      const result = await fetchTmuxControl();
      updateActivePaneRef(result.activePaneRef);
      renderTmuxWindows(result.windows || []);
    } catch (error) {
      console.error(error);
      renderTmuxWindows([]);
    }
  }

  async function fetchResizeSettings() {
    if (!activeId) return null;
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/resize`, { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`resize state request failed: ${response.status}`);
    }
    return response.json();
  }

  async function postResizeSettings(mode, viewerId) {
    if (!activeId) return null;
    const body = mutationPayload({ mode });
    if (mode === "fit-once" && viewerId) {
      body.viewerId = viewerId;
    }
    const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/resize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body)
    });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`resize apply request failed: ${response.status}`);
    }
    if (response.status === 204) return null;
    return response.json().catch(() => null);
  }

  async function refreshResizeSettings() {
    updateResizeControls(true);
    setResizeStatus(activeId ? "Loading..." : "No active session");
    if (!activeId) {
      resizeState = normalizeResizeState(null);
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = "";
      renderResizeSettings();
      setResizeStatus("No active session");
      updateResizeControls(false);
      return;
    }
    try {
      resizeState = normalizeResizeState(await fetchResizeSettings());
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = selectResizeViewerId(resizeState.selectedViewerId, resizeState.viewers);
      renderResizeSettings();
      setResizeStatus("");
    } catch (error) {
      console.error(error);
      resizeState = normalizeResizeState(resizeState);
      renderResizeSettings();
      setResizeStatus("Resize API unavailable");
    } finally {
      updateResizeControls(false);
    }
  }

  async function applyResizeSettings() {
    if (!activeId || resizeApplying) return;
    if (resizeDraftMode === "fit-once" && !resizeDraftViewerId) {
      setResizeStatus("Select a web window");
      return;
    }
    resizeApplying = true;
    updateResizeControls(true);
    setResizeStatus("Applying...");
    try {
      const applied = await postResizeSettings(resizeDraftMode, resizeDraftViewerId);
      if (applied) {
        resizeState = normalizeResizeState(applied);
      } else {
        resizeState = normalizeResizeState(await fetchResizeSettings());
      }
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = selectResizeViewerId(resizeState.selectedViewerId, resizeState.viewers);
      renderResizeSettings();
      requestTerminalResize();
      window.setTimeout(requestTerminalResize, 150);
      setResizeStatus("");
    } catch (error) {
      console.error(error);
      setResizeStatus("Apply failed");
    } finally {
      resizeApplying = false;
      updateResizeControls(false);
    }
  }

  function normalizeResizeState(payload) {
    const safe = payload && typeof payload === "object" ? payload : {};
    const mode = safe.mode === "fixed" ? "fixed" : "fixed";
    const viewers = Array.isArray(safe.viewers) ? safe.viewers.map(normalizeResizeViewer).filter(Boolean) : [];
    return {
      mode,
      selectedViewerId: typeof safe.selectedViewerId === "string" ? safe.selectedViewerId : "",
      viewers,
      window: safe.window && typeof safe.window === "object" ? safe.window : null,
      capabilities: Array.isArray(safe.capabilities) ? safe.capabilities : [],
      applied: safe.applied || null
    };
  }

  function normalizeResizeViewer(viewer) {
    if (!viewer || typeof viewer !== "object") return null;
    const id = typeof viewer.id === "string" ? viewer.id : "";
    if (!id) return null;
    return {
      id,
      ip: typeof viewer.ip === "string" ? viewer.ip : "",
      userAgent: typeof viewer.userAgent === "string" ? viewer.userAgent : "",
      width: Number.isFinite(Number(viewer.width)) ? Number(viewer.width) : 0,
      height: Number.isFinite(Number(viewer.height)) ? Number(viewer.height) : 0,
      lastSeen: viewer.lastSeen || "",
      active: viewer.active !== false
    };
  }

  function selectResizeViewerId(preferred, viewers) {
    if (preferred && viewers.some((viewer) => viewer.id === preferred)) return preferred;
    const current = viewers.find((viewer) => viewer.id === resizeViewerId);
    if (current) return current.id;
    const activeViewer = viewers.find((viewer) => viewer.active);
    if (activeViewer) return activeViewer.id;
    return viewers.length ? viewers[0].id : "";
  }

  function renderResizeSettings() {
    for (const input of resizeModes.querySelectorAll("input[name='resize-mode']")) {
      const selected = input.value === resizeDraftMode;
      input.checked = selected;
      const label = input.closest(".resize-mode-option");
      if (label) {
        label.classList.toggle("selected", selected);
      }
    }
    renderResizeViewers();
    renderResizePrimary();
    updateResizeControls(resizeApplying);
  }

  function renderResizeViewers() {
    resizeViewers.replaceChildren();
    const viewers = [...resizeState.viewers].sort((left, right) => {
      if (left.id === resizeViewerId) return -1;
      if (right.id === resizeViewerId) return 1;
      if (left.active !== right.active) return left.active ? -1 : 1;
      return String(right.lastSeen).localeCompare(String(left.lastSeen));
    });
    if (!viewers.length) {
      const empty = document.createElement("div");
      empty.className = "resize-empty";
      empty.textContent = "No web windows";
      resizeViewers.appendChild(empty);
      return;
    }
    for (const viewer of viewers) {
      const label = document.createElement("label");
      label.className = "resize-viewer";
      label.classList.toggle("inactive", !viewer.active);
      label.classList.toggle("selected", viewer.id === resizeDraftViewerId);
      label.title = viewer.userAgent || viewer.id;

      const input = document.createElement("input");
      input.type = "radio";
      input.name = "resize-viewer";
      input.value = viewer.id;
      input.checked = viewer.id === resizeDraftViewerId;
      label.appendChild(input);

      const body = document.createElement("span");
      body.className = "resize-viewer-body";

      const main = document.createElement("span");
      main.className = "resize-viewer-main";
      main.textContent = summarizeUserAgent(viewer.userAgent) || viewer.id;
      if (viewer.id === resizeViewerId) {
        const badge = document.createElement("span");
        badge.className = "resize-current-badge";
        badge.textContent = "current";
        main.appendChild(badge);
      }
      body.appendChild(main);

      const meta = document.createElement("span");
      meta.className = "resize-viewer-meta";
      meta.textContent = [viewer.ip, formatDimensions(viewer.width, viewer.height), formatLastSeen(viewer.lastSeen), viewer.active ? "" : "inactive"].filter(Boolean).join(" | ");
      body.appendChild(meta);

      label.appendChild(body);
      resizeViewers.appendChild(label);
    }
  }

  function renderResizePrimary() {
    resizePrimary.replaceChildren();
    if (!resizeState.window) {
      const empty = document.createElement("div");
      empty.className = "resize-empty";
      empty.textContent = "Window size unavailable";
      resizePrimary.appendChild(empty);
      return;
    }
    const currentWindow = resizeState.window;
    const row = document.createElement("div");
    row.className = "resize-primary-row";

    const name = document.createElement("div");
    name.className = "resize-primary-name";
    name.textContent = "Current tmux window";
    row.appendChild(name);

    const meta = document.createElement("div");
    meta.className = "resize-primary-meta";
    meta.textContent = formatDimensions(currentWindow.width, currentWindow.height);
    row.appendChild(meta);

    resizePrimary.appendChild(row);
  }

  function updateResizeControls(loading) {
    const disabled = loading || resizeApplying || !activeId;
    for (const input of resizeModes.querySelectorAll("input")) {
      const unsupported = input.value === "follow-device";
      input.disabled = disabled || unsupported;
      const label = input.closest(".resize-mode-option");
      if (label) {
        label.classList.toggle("disabled", disabled || unsupported);
      }
    }
    for (const input of resizeViewers.querySelectorAll("input")) {
      const viewerDisabled = disabled || resizeDraftMode !== "fit-once";
      input.disabled = viewerDisabled;
      const label = input.closest(".resize-viewer");
      if (label) {
        label.classList.toggle("disabled", viewerDisabled);
      }
    }
    resizeApply.disabled = disabled || resizeDraftMode === "follow-device" || (resizeDraftMode === "fit-once" && !resizeDraftViewerId);
  }

  function setResizeStatus(message) {
    resizeStatus.textContent = message || "";
  }

  function summarizeUserAgent(userAgent) {
    if (!userAgent) return "";
    const browser = userAgent.includes("Edg/") ? "Edge"
      : userAgent.includes("Firefox/") ? "Firefox"
        : userAgent.includes("Chrome/") || userAgent.includes("Chromium/") ? "Chrome"
          : userAgent.includes("Safari/") ? "Safari"
            : "Browser";
    const os = userAgent.includes("Windows") ? "Windows"
      : userAgent.includes("Mac OS X") || userAgent.includes("Macintosh") ? "macOS"
        : userAgent.includes("Android") ? "Android"
          : userAgent.includes("iPhone") || userAgent.includes("iPad") ? "iOS"
            : userAgent.includes("Linux") ? "Linux"
              : "";
    return [browser, os].filter(Boolean).join(" ");
  }

  function formatDimensions(width, height) {
    const cols = Number(width);
    const rows = Number(height);
    if (!Number.isFinite(cols) || !Number.isFinite(rows) || cols <= 0 || rows <= 0) return "";
    return `${Math.round(cols)}x${Math.round(rows)}`;
  }

  function formatLastSeen(value) {
    if (!value) return "";
    const rawTimestamp = typeof value === "number" ? value : Date.parse(value);
    const timestamp = rawTimestamp > 0 && rawTimestamp < 1000000000000 ? rawTimestamp * 1000 : rawTimestamp;
    if (!Number.isFinite(timestamp)) return String(value);
    const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
    if (seconds < 5) return "now";
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    return new Date(timestamp).toLocaleString();
  }

  function scheduleResizeViewerHeartbeat(delay) {
    window.clearTimeout(pendingViewerHeartbeatTimer);
    pendingViewerHeartbeatTimer = window.setTimeout(postResizeViewerHeartbeat, delay);
  }

  async function postResizeViewerHeartbeat() {
    if (!activeId || viewerHeartbeatInFlight) return;
    viewerHeartbeatInFlight = true;
    try {
      const response = await mutationFetch(`/api/sessions/${encodeURIComponent(activeId)}/resize/viewer`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(mutationPayload({ viewerId: resizeViewerId, ...getActiveTerminalDimensions(), transient: appViewportTransient }))
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`resize viewer heartbeat failed: ${response.status}`);
      }
      lastResizeViewerHeartbeatError = "";
    } catch (error) {
      const message = error && error.message ? error.message : String(error);
      if (message !== lastResizeViewerHeartbeatError) {
        console.error(error);
        lastResizeViewerHeartbeatError = message;
      }
    } finally {
      viewerHeartbeatInFlight = false;
    }
  }

  function getActiveTerminalDimensions() {
    const frame = frames.get(activeId);
    if (!frame) return { width: 0, height: 0 };
    const terminalSize = readXtermSize(frame);
    if (terminalSize) return terminalSize;
    const rect = frame.getBoundingClientRect();
    return {
      width: Math.max(0, Math.round(rect.width || frame.clientWidth || 0)),
      height: Math.max(0, Math.round(rect.height || frame.clientHeight || 0))
    };
  }

  function readXtermSize(frame) {
    try {
      const win = frame.contentWindow;
      const candidates = [win.term, win.terminal, win.xterm];
      for (const terminal of candidates) {
        const cols = Number(terminal && terminal.cols);
        const rows = Number(terminal && terminal.rows);
        if (cols > 0 && rows > 0) {
          return { width: Math.round(cols), height: Math.round(rows) };
        }
      }
      const rowsElement = frame.contentDocument && frame.contentDocument.querySelector(".xterm-rows");
      if (!rowsElement) return null;
      const rowElements = Array.from(rowsElement.children);
      const rows = rowElements.length;
      const cols = rowElements.reduce((max, row) => Math.max(max, row.textContent.length), 0);
      if (cols > 0 && rows > 0) {
        return { width: cols, height: rows };
      }
    } catch (error) {
      return null;
    }
    return null;
  }

  function focusActiveTerminal() {
    if (preserveOpenDialogFocus()) return;
    const frame = frames.get(activeId);
    if (!frame) return;
    frame.focus();
    try {
      frame.contentWindow.focus();
      const terminal = frame.contentWindow.term || frame.contentWindow.terminal || frame.contentWindow.xterm;
      if (terminal && typeof terminal.focus === "function") terminal.focus();
    } catch (error) {
      // Restoring focus is best effort if the embedded terminal is unavailable.
    }
  }

  function renderKeyButtons() {
    keyGrid.replaceChildren();
    for (const key of specialKeys) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = key.urgent ? "key-button urgent" : "key-button";
      button.dataset.key = key.key;
      button.textContent = key.label;
      button.title = key.title;
      button.addEventListener("click", () => postKey(key.key));
      keyGrid.appendChild(button);
    }
    updateKeyButtons(false);
  }

  function renderControlActions() {
    tControlGrid.replaceChildren();
    for (const control of tmuxControls) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = control.urgent ? "key-button urgent" : "key-button";
      button.dataset.controlAction = control.action;
      button.textContent = control.label;
      button.title = control.title;
      button.addEventListener("click", () => runControlAction(control));
      tControlGrid.appendChild(button);
    }
    updateControlButtons();
  }

  function renderTmuxWindows(windows) {
    tmuxWindows = windows;
    updateSessionTabBadge(activeId, windows.length);
    if (!windows.length) {
      tControlWindows.replaceChildren();
      const empty = document.createElement("div");
      empty.className = "tcontrol-empty";
      empty.textContent = "No tmux windows";
      tControlWindows.appendChild(empty);
      return;
    }

    const existingButtons = new Map();
    for (const child of tControlWindows.children) {
      if (child instanceof HTMLButtonElement && child.classList.contains("tcontrol-window")) {
        existingButtons.set(child.dataset.windowRef || "", child);
      }
    }
    const renderedButtons = new Set();
    windows.forEach((tmuxWindow, index) => {
      const windowRef = String(tmuxWindow.ref || "");
      let button = existingButtons.get(windowRef);
      if (!button) {
        button = document.createElement("button");
        button.type = "button";
        button.className = "tcontrol-window";
        button.addEventListener("click", (event) => {
          const selectedWindowRef = event.currentTarget.dataset.windowRef;
          if (selectedWindowRef) {
            postTmuxControl("select-window", { windowRef: selectedWindowRef });
          }
        });
      }
      renderedButtons.add(button);
      button.classList.toggle("active", Boolean(tmuxWindow.active));
      if (button.dataset.windowRef !== windowRef) button.dataset.windowRef = windowRef;
      const title = `${tmuxWindow.panes} pane${tmuxWindow.panes === 1 ? "" : "s"}`;
      if (button.title !== title) button.title = title;
      const label = tmuxWindow.name || "(unnamed)";
      if (button.textContent !== label) button.textContent = label;
      const currentButton = tControlWindows.children[index];
      if (currentButton !== button) {
        tControlWindows.insertBefore(button, currentButton || null);
      }
    });
    for (const child of Array.from(tControlWindows.children)) {
      if (!renderedButtons.has(child)) child.remove();
    }
    updateControlButtons();
  }

  function runControlAction(control) {
    const payload = {};
    if (control.confirm && !window.confirm(control.confirm)) {
      return;
    }
    if (control.prompt) {
      const activeWindow = tmuxWindows.find((tmuxWindow) => tmuxWindow.active);
      const name = window.prompt("Window name", activeWindow ? activeWindow.name : "");
      if (name === null) return;
      payload.name = name;
    }
    postTmuxControl(control.action, payload);
  }

  function updateKeyButtons(sending) {
    const disabled = sending || !activeId || frameTransport.get(activeId) !== TransportState.CONNECTED;
    for (const button of keyGrid.querySelectorAll("button")) {
      button.disabled = disabled;
    }
  }

  function updateControlButtons() {
    const disabled = tmuxControlSubmitting || !activeId;
    for (const button of tControlGrid.querySelectorAll("button")) {
      button.disabled = disabled;
    }
    for (const button of tControlWindows.querySelectorAll("button")) {
      button.disabled = disabled;
    }
  }

  function setKeyPanelOpen(open) {
    keyPanel.hidden = !open;
    keysToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setTControlPanelOpen(false);
      setResizePanelOpen(false);
      updateKeyButtons(false);
    }
  }

  function setTControlPanelOpen(open) {
    tControlPanel.hidden = !open;
    tControlToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setKeyPanelOpen(false);
      setResizePanelOpen(false);
      refreshTmuxControl();
    }
  }

  function setResizePanelOpen(open) {
    resizePanel.hidden = !open;
    resizeToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setKeyPanelOpen(false);
      setTControlPanelOpen(false);
      postResizeViewerHeartbeat().finally(refreshResizeSettings);
    }
  }

  function setActionsMenuOpen(open) {
    actionsPopover.hidden = !open;
    actionsToggle.setAttribute("aria-expanded", String(open));
  }

  historyOverlay.addEventListener("scroll", () => {
    if (historyOverlay.scrollTop < 700) loadOlderHistory();
  }, { passive: true });

  document.addEventListener("selectionchange", () => {
    if (!historyIsOpen() || uiState === UIState.HISTORY_LOADING || uiState === UIState.PASTE_PENDING) return;
    const selected = historySelectionActive();
    setUIState(selected ? UIState.COPY : UIState.HISTORY);
    if (!selected && historyMaterializePending) {
      historyMaterializePending = false;
      loadOlderHistory();
    }
  });

  historyNewOutput.addEventListener("click", () => closeHistory());
  historyClose.addEventListener("click", () => closeHistory());
  historyPaste.addEventListener("click", pasteFromClipboard);
  historyReflow.addEventListener("click", () => {
    if (history.mode !== "reflow" && !historyReflow.disabled) openHistory("reflow");
  });
  historyFixed.addEventListener("click", () => {
    if (history.mode !== "fixed") openHistory("fixed");
  });
  actionsToggle.addEventListener("click", () => setActionsMenuOpen(actionsPopover.hidden));
  logoutForm.addEventListener("submit", logout);
  newSessionToggle.addEventListener("click", openCreateSessionDialog);
  terminateSessionToggle.addEventListener("click", openTerminateSessionDialog);
  createSessionForm.addEventListener("submit", createManagedSession);
  createSessionName.addEventListener("input", () => {
    setCreateStatus(createSessionName.value ? validateSessionName(createSessionName.value) : "");
  });
  createSessionCancel.addEventListener("click", closeCreateSessionDialog);
  createSessionDialog.addEventListener("cancel", (event) => {
    if (createSubmitting) event.preventDefault();
  });
  createSessionDialog.addEventListener("close", () => {
    if (!createSubmitting) {
      createSessionForm.reset();
      setCreateStatus("");
    }
    restoreDialogFocus(createDialogOpener);
    createDialogOpener = null;
  });
  terminateSessionForm.addEventListener("submit", terminateManagedSession);
  terminateSessionCancel.addEventListener("click", closeTerminateSessionDialog);
  terminateSessionDialog.addEventListener("cancel", (event) => {
    if (terminateSubmitting) event.preventDefault();
  });
  terminateSessionDialog.addEventListener("close", () => {
    if (!terminateSubmitting) setTerminateStatus("");
    terminateTarget = null;
    terminateSessionName.textContent = "";
    restoreDialogFocus(terminateDialogOpener);
    terminateDialogOpener = null;
  });
  keysToggle.addEventListener("click", () => {
    setKeyPanelOpen(true);
    setActionsMenuOpen(false);
  });
  tControlToggle.addEventListener("click", () => {
    setTControlPanelOpen(true);
    setActionsMenuOpen(false);
  });
  resizeToggle.addEventListener("click", () => {
    setResizePanelOpen(true);
    setActionsMenuOpen(false);
  });
  historyToggle.addEventListener("click", () => {
    if (historyIsOpen()) {
      closeHistory();
    } else {
      openHistory("reflow");
    }
    setActionsMenuOpen(false);
  });
  pasteToggle.addEventListener("click", () => {
    setActionsMenuOpen(false);
    pasteFromClipboard();
  });
  scrollGestureModeControl.addEventListener("change", () => {
    setScrollGestureMode(scrollGestureModeControl.value);
  });
  historyPasteFallback.addEventListener("paste", (event) => {
    if (!historyIsOpen()) return;
    if (event.clipboardData) {
      const text = event.clipboardData.getData("text/plain");
      if (text) {
        event.preventDefault();
        stagePaste(text);
        return;
      }
    }
    window.setTimeout(() => {
      const text = historyPasteFallback.value;
      historyPasteFallback.value = "";
      stagePaste(text);
    }, 0);
  });
  pasteConfirmForm.addEventListener("submit", confirmPaste);
  pasteConfirmCancel.addEventListener("click", cancelPasteConfirmation);
  pasteConfirmDialog.addEventListener("cancel", (event) => {
    if (pasteSubmitting) event.preventDefault();
  });
  pasteConfirmDialog.addEventListener("close", () => {
    if (pasteTransitioning || !historyIsOpen()) return;
    clearStagedPaste();
    setUIState(historySelectionActive() ? UIState.COPY : UIState.HISTORY);
    showHistoryPasteFallback("Paste canceled. Nothing was sent.", true);
  });
  keysClose.addEventListener("click", () => setKeyPanelOpen(false));
  tControlClose.addEventListener("click", () => setTControlPanelOpen(false));
  resizeClose.addEventListener("click", () => setResizePanelOpen(false));
  resizeModes.addEventListener("change", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || target.name !== "resize-mode") return;
    resizeDraftMode = target.value;
    if (resizeDraftMode === "fit-once" && !resizeDraftViewerId) {
      resizeDraftViewerId = selectResizeViewerId("", resizeState.viewers);
    }
    renderResizeSettings();
  });
  resizeViewers.addEventListener("change", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || target.name !== "resize-viewer") return;
    resizeDraftViewerId = target.value;
    renderResizeSettings();
  });
  resizeApply.addEventListener("click", applyResizeSettings);
  document.addEventListener("focusin", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLElement)) return;
    const dialog = target.closest("dialog");
    if (dialog && dialog.open) dialogFocusOwner = target;
  });
  document.addEventListener("click", (event) => {
    if (actionsPopover.hidden || actionsMenu.contains(event.target)) return;
    setActionsMenuOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (createSessionDialog.open || terminateSessionDialog.open || pasteConfirmDialog.open) return;
    if (!historyIsOpen() && scrollGestureMode === "history" && event.key === "PageUp" && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault();
      event.stopPropagation();
      openHistory("reflow");
      return;
    }
    if (historyIsOpen()) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "c" && historySelectionActive()) return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        closeHistory();
        return;
      }
      const printable = [...event.key].length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey;
      if (printable || event.key === "Enter" || event.key === "Backspace") {
        event.preventDefault();
        event.stopPropagation();
        sendFirstHistoryInput(event.key);
        return;
      }
    }
    if (event.key !== "Escape") return;
    setActionsMenuOpen(false);
    setKeyPanelOpen(false);
    setTControlPanelOpen(false);
    setResizePanelOpen(false);
  });

  renderKeyButtons();
  renderControlActions();
  setScrollGestureMode(scrollGestureMode);
  window.addEventListener("message", handleTerminalTransportMessage);
  installAppViewportTracking();
  refreshVersion();
  refresh();
  window.setInterval(refresh, 3000);
  window.setInterval(postResizeViewerHeartbeat, 3000);
})();
