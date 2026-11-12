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

  // FORMATS are the level formats the admin page can write.
  var FORMATS = ["blind-nl", "blind-limit", "stud-limit"];

  // parseLevels reads the levels text area.
  //
  // Every line is either "sb,bb,ante,minutes" or "break,minutes". The
  // minutes are converted to seconds.
  //
  // A level line can carry the optional prefixes "game=<name>" and
  // "fmt=<format>" (in any order, both optional). The format decides
  // the shape of the comma list:
  //
  //   blind-nl:    sb,bb,ante,minutes
  //   blind-limit: sb,bb,big_bet,ante,minutes
  //   stud-limit:  bring_in,complete,ante,minutes
  //
  // An empty or "-" big bet means 0: the display derives it from the BB.
  function parseLevels(text) {
    var levels = [];
    var lines = text.split("\n");
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line) continue;
      levels.push(parseLevel(line, i + 1));
    }
    if (levels.length === 0) {
      throw new Error("the tournament needs at least one level");
    }
    return levels;
  }

  // parseLevel parses one line of the levels text area.
  function parseLevel(line, lineNo) {
    var tokens = line.split(/\s+/).filter(function (token) {
      return token !== "";
    });
    var game = "";
    var format = "";
    var data = "";
    for (var i = 0; i < tokens.length; i++) {
      var token = tokens[i];
      var eq = token.indexOf("=");
      if (eq > 0) {
        var key = token.slice(0, eq);
        var value = token.slice(eq + 1);
        if (key === "game") {
          if (!value) {
            throw new Error("line " + lineNo + ": the game name is empty");
          }
          game = value;
          continue;
        }
        if (key === "fmt") {
          if (FORMATS.indexOf(value) < 0) {
            throw new Error("line " + lineNo + ": unknown format " + value +
              " (blind-nl, blind-limit or stud-limit)");
          }
          format = value;
          continue;
        }
        throw new Error("line " + lineNo + ": unknown prefix " + key + " (game or fmt)");
      }
      if (data) {
        throw new Error("line " + lineNo + ": only one comma list per line");
      }
      data = token;
    }
    if (!data) {
      throw new Error("line " + lineNo + ": the line has no level");
    }

    var parts = data.split(",").map(function (part) {
      return part.trim();
    });
    var minutes = parseFloat(parts[parts.length - 1]);
    if (isNaN(minutes) || minutes <= 0) {
      throw new Error("line " + lineNo + ": the duration must be a positive number of minutes");
    }
    var duration = Math.round(minutes * 60);

    if (parts[0].toLowerCase() === "break") {
      return { type: "break", duration_sec: duration };
    }

    if (!format) format = "blind-nl";
    if (format === "blind-limit") {
      if (parts.length !== 5) {
        throw new Error("line " + lineNo + ": expected sb,bb,big_bet,ante,minutes");
      }
      var sb = parseInt(parts[0], 10);
      var bb = parseInt(parts[1], 10);
      var bigBet = (parts[2] === "" || parts[2] === "-") ? 0 : parseInt(parts[2], 10);
      var ante = parseInt(parts[3], 10);
      if (isNaN(sb) || isNaN(bb) || isNaN(bigBet) || isNaN(ante)) {
        throw new Error("line " + lineNo + ": the forced bets must be numbers");
      }
      return level("blind-limit", game, {
        sb: sb, bb: bb, big_bet: bigBet, ante: ante
      }, duration);
    }
    if (format === "stud-limit") {
      if (parts.length !== 4) {
        throw new Error("line " + lineNo + ": expected bring_in,complete,ante,minutes");
      }
      var bringIn = parseInt(parts[0], 10);
      var complete = parseInt(parts[1], 10);
      var ante = parseInt(parts[2], 10);
      if (isNaN(bringIn) || isNaN(complete) || isNaN(ante)) {
        throw new Error("line " + lineNo + ": the forced bets must be numbers");
      }
      return level("stud-limit", game, {
        bring_in: bringIn, complete: complete, ante: ante
      }, duration);
    }
    // blind-nl
    if (parts.length !== 4) {
      throw new Error("line " + lineNo + ": expected sb,bb,ante,minutes");
    }
    var sb = parseInt(parts[0], 10);
    var bb = parseInt(parts[1], 10);
    var ante = parseInt(parts[2], 10);
    if (isNaN(sb) || isNaN(bb) || isNaN(ante)) {
      throw new Error("line " + lineNo + ": the blinds must be numbers");
    }
    return level("blind-nl", game, { sb: sb, bb: bb, ante: ante }, duration);
  }

  // level builds one level row of the structure.
  function level(format, game, bets, duration) {
    var lvl = { type: "level", format: format, bets: bets, duration_sec: duration };
    if (game) lvl.game = game;
    return lvl;
  }

  // gameMix reads the game mix settings of the create form.
  //
  // The games are a comma separated list of game types; the list may be
  // empty for a tournament without a mix. One level per game is the
  // default of the rotation.
  function gameMix() {
    var games = document.getElementById("games").value.split(",").map(function (game) {
      return game.trim();
    }).filter(function (game) {
      return game !== "";
    });
    var levelsPerGame = parseInt(document.getElementById("levels_per_game").value, 10);
    if (isNaN(levelsPerGame) || levelsPerGame < 1) levelsPerGame = 1;
    return {
      games: games,
      stud_enabled: document.getElementById("stud_enabled").checked,
      stud_show_big_bet: document.getElementById("stud_show_big_bet").checked,
      levels_per_game: levelsPerGame
    };
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
      rebuy_stack: parseInt(document.getElementById("rebuy_stack").value, 10) || 0,
      addon_chips: parseInt(document.getElementById("addon_chips").value, 10) || 0,
      levels: levels,
      game_mix: gameMix()
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

  // describe writes the forced bets of a level the way its format asks
  // for it (spec §7.7).
  //
  // The BIG BET is derived when the row has none: the BB times two for a
  // blind limit game, the COMPLETE times two for a stud game.
  function describeLevel(level, mix) {
    var bets = level.bets || {};
    var format = level.format || "blind-nl";
    var parts = [];
    if (format === "blind-limit") {
      pushBet(parts, "SB", bets.sb);
      pushBet(parts, "BB", bets.bb);
      pushBet(parts, "BIG BET", bigBet(bets, bets.bb));
      pushBet(parts, "ANTE", bets.ante);
    } else if (format === "stud-limit") {
      pushBet(parts, "BRING-IN", bets.bring_in);
      pushBet(parts, "COMPLETE", bets.complete);
      pushBet(parts, "ANTE", bets.ante);
      // A stud game shows the BIG BET only when it is asked for.
      if (mix && mix.stud_show_big_bet) {
        pushBet(parts, "BIG BET", bigBet(bets, bets.complete));
      }
    } else {
      pushBet(parts, "SB", bets.sb);
      pushBet(parts, "BB", bets.bb);
      pushBet(parts, "ANTE", bets.ante);
    }
    return parts.join(" / ");
  }

  // bigBet returns the BIG BET of a row: its own value, or the fallback
  // (the BB or the COMPLETE) times two.
  function bigBet(bets, fallback) {
    if (bets.big_bet) return bets.big_bet;
    return (fallback || 0) * 2;
  }

  // pushBet adds a forced bet to the parts, unless it is not set.
  function pushBet(parts, label, value) {
    if (!value) return;
    parts.push(label + " " + value);
  }

  function describe(snap) {
    var level = snap.current_level;
    var parts = [];
    if (level) {
      if (level.type === "break") {
        parts.push("Break");
      } else {
        parts.push("Level " + (level.index + 1) +
          (level.game ? " — " + level.game : "") + ": " +
          describeLevel(level, snap.game_mix));
      }
    }
    if (snap.next_level) {
      parts.push(snap.next_level.type === "break"
        ? "next: break"
        : "next: " +
          (snap.next_level.game ? snap.next_level.game + " — " : "") +
          describeLevel(snap.next_level, snap.game_mix));
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
