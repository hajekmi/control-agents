(function () {
  const tabs = document.getElementById("tabs");
  const pane = document.getElementById("terminal-pane");
  const emptyState = document.getElementById("empty-state");
  const status = document.getElementById("status");
  const frames = new Map();
  let activeId = "";

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

  function activate(id) {
    activeId = id;
    for (const button of tabs.querySelectorAll("button")) {
      button.classList.toggle("active", button.dataset.sessionId === id);
    }
    for (const [frameId, frame] of frames.entries()) {
      frame.hidden = frameId !== id;
    }
    emptyState.hidden = frames.size !== 0;
  }

  function render(sessions) {
    const nextIds = new Set(sessions.map((session) => session.id));
    for (const [id, frame] of frames.entries()) {
      if (!nextIds.has(id)) {
        frame.remove();
        frames.delete(id);
      }
    }

    tabs.replaceChildren();
    for (const session of sessions) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = session.name || session.id;
      button.dataset.sessionId = session.id;
      button.title = session.cwd || session.tmuxName || session.id;
      button.addEventListener("click", () => activate(session.id));
      tabs.appendChild(button);

      if (!frames.has(session.id)) {
        const frame = document.createElement("iframe");
        frame.className = "terminal-frame";
        frame.title = session.name || session.id;
        frame.src = `/terminal/${encodeURIComponent(session.id)}/`;
        frame.hidden = true;
        pane.appendChild(frame);
        frames.set(session.id, frame);
      }
    }

    if (!activeId || !nextIds.has(activeId)) {
      activeId = sessions.length > 0 ? sessions[0].id : "";
    }
    if (activeId) {
      activate(activeId);
    } else {
      emptyState.hidden = false;
    }
    status.textContent = `${sessions.length} active session${sessions.length === 1 ? "" : "s"}`;
  }

  async function refresh() {
    try {
      render(await fetchSessions());
    } catch (error) {
      status.textContent = "Failed to refresh sessions";
      console.error(error);
    }
  }

  refresh();
  window.setInterval(refresh, 3000);
})();

