// PonyTimer management page.
//
// The page talks to the same REST API as any other client: it lists the
// tournaments, creates new ones and sends the commands of spec §6.

(function () {
  "use strict";

  var listEl = document.getElementById("tournaments");
  var formEl = document.getElementById("create-form");
  var errorEl = document.getElementById("create-error");

  // refreshInterval reloads the tournament list, so that a second browser
  // tab stays up to date.
  var refreshInterval = 5000;

  formEl.addEventListener("submit", function (event) {
    event.preventDefault();
    createTournament();
  });

  function showError(message) {
    errorEl.textContent = message;
    errorEl.hidden = false;
  }

  function clearError() {
    errorEl.hidden = true;
    errorEl.textContent = "";
  }

  // parseLevels reads the levels text area.
  //
  // Every line is either "sb,bb,ante,minutes" or "break,minutes". The
  // minutes are converted to seconds.
  function parseLevels(text) {
    var levels = [];
    var lines = text.split("\n");
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line) continue;
      var parts = line.split(",").map(function (part) {
        return part.trim();
      });
      var minutes = parseFloat(parts[parts.length - 1]);
      if (isNaN(minutes) || minutes <= 0) {
        throw new Error("line " + (i + 1) + ": the duration must be a positive number of minutes");
      }
      var duration = Math.round(minutes * 60);
      if (parts[0].toLowerCase() === "break") {
        levels.push({ type: "break", sb: 0, bb: 0, ante: 0, duration_sec: duration });
        continue;
      }
      if (parts.length !== 4) {
        throw new Error("line " + (i + 1) + ": expected sb,bb,ante,minutes");
      }
      var sb = parseInt(parts[0], 10);
      var bb = parseInt(parts[1], 10);
      var ante = parseInt(parts[2], 10);
      if (isNaN(sb) || isNaN(bb) || isNaN(ante)) {
        throw new Error("line " + (i + 1) + ": the blinds must be numbers");
      }
      levels.push({ type: "level", sb: sb, bb: bb, ante: ante, duration_sec: duration });
    }
    if (levels.length === 0) {
      throw new Error("the tournament needs at least one level");
    }
    return levels;
  }

  function createTournament() {
    clearError();
    var levels;
    try {
      levels = parseLevels(document.getElementById("levels").value);
    } catch (err) {
      showError(err.message);
      return;
    }
    var body = {
      name: document.getElementById("name").value.trim(),
      starting_stack: parseInt(document.getElementById("starting_stack").value, 10) || 0,
      addon_chips: parseInt(document.getElementById("addon_chips").value, 10) || 0,
      levels: levels
    };
    if (!body.name) {
      showError("the tournament needs a name");
      return;
    }
    fetch("/api/tournaments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    })
      .then(function (res) {
        if (res.status === 401) {
          throw new Error("the session expired — please sign in again");
        }
        if (!res.ok) {
          return res.json().then(function (data) {
            throw new Error(data.error || "cannot create the tournament");
          });
        }
        return res.json();
      })
      .then(function () {
        document.getElementById("name").value = "";
        refresh();
      })
      .catch(function (err) {
        showError(err.message);
      });
  }

  // refresh reloads the list of tournaments.
  function refresh() {
    fetch("/api/tournaments", { cache: "no-store" })
      .then(function (res) {
        if (res.status === 401) {
          throw new Error("the session expired — please sign in again");
        }
        if (!res.ok) throw new Error("cannot load the tournaments");
        return res.json();
      })
      .then(function (summaries) {
        listEl.textContent = "";
        if (!summaries.length) {
          var empty = document.createElement("p");
          empty.className = "sub";
          empty.textContent = "No tournaments yet.";
          listEl.appendChild(empty);
          return;
        }
        summaries.forEach(function (summary) {
          listEl.appendChild(tournamentEl(summary));
        });
      })
      .catch(function (err) {
        listEl.textContent = "";
        var p = document.createElement("p");
        p.className = "error";
        p.textContent = err.message;
        listEl.appendChild(p);
      });
  }

  // tournamentEl builds the control panel of one tournament.
  function tournamentEl(summary) {
    var el = document.createElement("div");
    el.className = "tournament";
    el.setAttribute("data-id", summary.id);

    var head = document.createElement("div");
    head.className = "tournament-head";

    var title = document.createElement("h3");
    title.textContent = summary.name;

    var state = document.createElement("span");
    state.className = "state";
    state.setAttribute("data-state", summary.state);
    state.textContent = summary.state;

    var link = document.createElement("span");
    link.className = "display-link";
    var a = document.createElement("a");
    a.href = "/display?token=" + encodeURIComponent(summary.display_token);
    a.target = "_blank";
    a.rel = "noopener";
    a.textContent = "Open display";
    link.appendChild(a);

    head.appendChild(title);
    head.appendChild(state);
    head.appendChild(link);
    el.appendChild(head);

    // The clock and the levels of the current level.
    var info = document.createElement("div");
    info.className = "sub";
    info.textContent = "";
    el.appendChild(info);

    var controls = document.createElement("div");
    controls.className = "controls";
    controls.appendChild(commandButton("Start", summary.id, "start"));
    controls.appendChild(commandButton("Pause", summary.id, "pause"));
    controls.appendChild(commandButton("Resume", summary.id, "resume"));
    controls.appendChild(commandButton("◀ Level", summary.id, "prev_level"));
    controls.appendChild(commandButton("Level ▶", summary.id, "next_level"));
    [30, 5, -5, -10, -30].forEach(function (delta) {
      controls.appendChild(
        commandButton(delta > 0 ? "+" + delta + "s" : delta + "s", summary.id, "adjust", delta)
      );
    });
    el.appendChild(controls);

    var counts = document.createElement("div");
    counts.className = "counts";
    el.appendChild(counts);

    // Load the snapshot of the tournament to fill in the counters.
    loadSnapshot(summary.id).then(function (snap) {
      state.setAttribute("data-state", snap.state);
      state.textContent = snap.state;
      info.textContent = describe(snap);
      counts.textContent = "";
      [
        ["entries", "Entries"],
        ["players_left", "Players left"],
        ["rebuys", "Rebuys"],
        ["addons", "Addons"],
        ["reentries", "Reentries"]
      ].forEach(function (entry) {
        counts.appendChild(counterEl(summary.id, entry[0], entry[1], snap[entry[0]] || 0));
      });
    });

    return el;
  }

  function describe(snap) {
    var level = snap.current_level;
    var parts = [];
    if (level) {
      if (level.type === "break") {
        parts.push("Break");
      } else {
        parts.push("Level " + (level.index + 1) + ": SB " + level.sb + " / BB " + level.bb +
          (level.ante ? " / Ante " + level.ante : ""));
      }
    }
    if (snap.next_level) {
      parts.push(snap.next_level.type === "break"
        ? "next: break"
        : "next: SB " + snap.next_level.sb + " / BB " + snap.next_level.bb);
    }
    if (snap.state === "paused") {
      parts.push("paused with " + Math.round(snap.remaining_sec) + "s left");
    }
    return parts.join(" — ");
  }

  function commandButton(label, id, type, delta) {
    var button = document.createElement("button");
    button.type = "button";
    button.textContent = label;
    button.addEventListener("click", function () {
      var body = { type: type };
      if (type === "adjust") body.delta_sec = delta;
      sendCommand(id, body, button);
    });
    return button;
  }

  // counterEl builds a stepper of one counter.
  function counterEl(id, field, label, value) {
    var el = document.createElement("div");
    el.className = "counter";

    var title = document.createElement("div");
    title.className = "counter-label";
    title.textContent = label;

    var number = document.createElement("div");
    number.className = "counter-value";
    number.textContent = String(value);

    var minus = document.createElement("button");
    minus.type = "button";
    minus.textContent = "−";
    var plus = document.createElement("button");
    plus.type = "button";
    plus.textContent = "+";

    function step(delta) {
      var counts = {};
      counts[field] = { delta: delta };
      sendCommand(id, { type: "counts", counts: counts }, null).then(function () {
        loadSnapshot(id).then(function (snap) {
          number.textContent = String(snap[field] || 0);
        });
      });
    }
    minus.addEventListener("click", function () { step(-1); });
    plus.addEventListener("click", function () { step(1); });

    el.appendChild(title);
    el.appendChild(number);
    el.appendChild(minus);
    el.appendChild(plus);
    return el;
  }

  function loadSnapshot(id) {
    return fetch("/api/tournaments/" + encodeURIComponent(id) + "/snapshot", { cache: "no-store" })
      .then(function (res) {
        if (!res.ok) throw new Error("cannot load the snapshot");
        return res.json();
      });
  }

  function sendCommand(id, body, button) {
    if (button) button.disabled = true;
    return fetch("/api/tournaments/" + encodeURIComponent(id) + "/commands", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    })
      .then(function (res) {
        if (!res.ok) {
          return res.json().then(function (data) {
            throw new Error(data.error || "the command failed");
          });
        }
        return res.json();
      })
      .catch(function (err) {
        window.alert(err.message);
      })
      .finally(function () {
        if (button) button.disabled = false;
        refresh();
      });
  }

  refresh();
  setInterval(refresh, refreshInterval);
})();
