// PonyTimer display page.
//
// The page is read-only: it fetches the snapshot of the tournament that the
// display token points at, subscribes to its event stream and renders the
// clock locally between two events.
//
// Sound: v0.1 has no sound at all. When sound is added later, only slot 1
// will ever play it, so that two tables reaching a level up at the same
// moment do not shout over each other (spec §7.4).

(function () {
  "use strict";

  // The clock is redrawn every 200 ms: fast enough to look smooth, slow
  // enough to keep the CPU of a venue machine low.
  var TICK_MS = 200;
  // The snapshot is refreshed periodically, so that a display recovers even
  // if it missed an event.
  var REFRESH_MS = 30000;

  var params = new URLSearchParams(window.location.search);
  var tokens = params.getAll("token");
  var wanted = parseInt(params.get("slots") || "", 10);
  // The slot count comes from the "slots" parameter; without it the number
  // of tokens decides. Slots without a token stay empty.
  var slotCount = wanted === 1 || wanted === 2 || wanted === 4 ? wanted : Math.max(1, tokens.length);

  var container = document.getElementById("slots");
  container.setAttribute("data-slots", String(slotCount));

  var slots = [];
  for (var i = 0; i < slotCount; i++) {
    slots.push(createSlot(container, tokens[i] || "", i));
  }

  function createSlot(parent, token, index) {
    var el = document.createElement("div");
    el.className = "slot";
    el.innerHTML =
      '<div class="slot-name"></div>' +
      '<div class="slot-main">' +
      '  <div class="slot-level"></div>' +
      '  <div class="slot-time"></div>' +
      '  <div class="slot-message"></div>' +
      '  <div class="slot-blinds"></div>' +
      "</div>" +
      '<div class="slot-stats"></div>' +
      '<div class="overlay" hidden>' +
      '  <div class="overlay-level"></div>' +
      '  <div class="overlay-title"></div>' +
      '  <div class="overlay-time"></div>' +
      '  <div class="overlay-sub"></div>' +
      "</div>";
    parent.appendChild(el);

    var slot = {
      token: token,
      index: index,
      el: el,
      name: el.querySelector(".slot-name"),
      level: el.querySelector(".slot-level"),
      time: el.querySelector(".slot-time"),
      message: el.querySelector(".slot-message"),
      blinds: el.querySelector(".slot-blinds"),
      stats: el.querySelector(".slot-stats"),
      overlay: el.querySelector(".overlay"),
      overlayLevel: el.querySelector(".overlay-level"),
      overlayTitle: el.querySelector(".overlay-title"),
      overlayTime: el.querySelector(".overlay-time"),
      overlaySub: el.querySelector(".overlay-sub"),
      snapshot: null,
      // failed marks a slot whose tournament could not be read; it stays
      // empty instead of showing a placeholder clock.
      failed: false,
      // offset is the difference between the server clock and the browser
      // clock, in milliseconds. It is refreshed with every snapshot.
      offset: 0,
      stream: null
    };

    if (!token) {
      el.classList.add("empty");
      slot.name.textContent = "";
      slot.time.textContent = "";
      return slot;
    }

    loadSnapshot(slot);
    connectStream(slot);
    // The periodic refresh also covers a display that was left open while
    // the server was restarted.
    setInterval(function () { loadSnapshot(slot); }, REFRESH_MS);
    return slot;
  }

  // loadSnapshot fetches the current snapshot of the tournament and stores
  // it in the slot.
  function loadSnapshot(slot) {
    return fetch("/api/display/" + encodeURIComponent(slot.token) + "/snapshot", {
      cache: "no-store"
    })
      .then(function (res) {
        if (!res.ok) throw new Error("snapshot status " + res.status);
        return res.json();
      })
      .then(function (snap) {
        slot.failed = false;
        slot.el.classList.remove("unavailable");
        applySnapshot(slot, snap);
      })
      .catch(function () {
        // A failing refresh must not wipe the last known state; the next
        // attempt will try again. A slot that never had a snapshot stays
        // empty.
        if (!slot.snapshot) {
          slot.failed = true;
          slot.el.classList.add("unavailable");
          slot.time.textContent = "";
        }
      });
  }

  // connectStream subscribes to the event stream of the tournament. The
  // browser reconnects on its own, and every reconnect refreshes the
  // snapshot, so a display that was disconnected catches up.
  function connectStream(slot) {
    var url = "/api/display/" + encodeURIComponent(slot.token) + "/events";
    var stream = new EventSource(url);
    slot.stream = stream;

    stream.onopen = function () {
      loadSnapshot(slot);
    };
    stream.onmessage = function (event) {
      var envelope;
      try {
        envelope = JSON.parse(event.data);
      } catch (err) {
        return;
      }
      if (envelope && envelope.payload) {
        applySnapshot(slot, envelope.payload);
      }
    };
    stream.onerror = function () {
      // EventSource reconnects by itself; the refresh on open resynchronises
      // the clock.
    };
  }

  // applySnapshot stores a snapshot and redraws the slot.
  function applySnapshot(slot, snap) {
    if (!snap) return;
    slot.snapshot = snap;
    var serverTime = snap.server_time !== undefined ? snap.server_time : snap.server_time_ms;
    if (typeof serverTime === "number") {
      // The browser clock and the server clock usually differ by a few
      // hundred milliseconds; the offset corrects the countdown.
      slot.offset = serverTime - Date.now();
    }
    render(slot);
  }

  // render draws the slot from the last snapshot and the local clock.
  function render(slot) {
    var snap = slot.snapshot;
    if (!snap) {
      // No snapshot yet: an empty slot stays empty, a slot whose
      // tournament cannot be read shows a placeholder.
      slot.time.textContent = slot.failed ? "" : "--:--";
      return;
    }

    var level = snap.current_level;
    slot.name.textContent = snap.tournament_name || "";
    setLevel(slot, level);

    var state = snap.state;
    var remaining = remainingSeconds(slot, snap);

    slot.message.textContent = "";
    slot.overlay.hidden = true;

    if (state === "waiting") {
      slot.time.textContent = "";
      slot.message.textContent = "Waiting To Begin";
      slot.blinds.innerHTML = "";
      slot.stats.innerHTML = statsHTML(snap);
      return;
    }
    if (state === "finished") {
      slot.time.textContent = "";
      slot.message.textContent = "Finished";
      slot.blinds.innerHTML = "";
      slot.stats.innerHTML = statsHTML(snap);
      return;
    }

    slot.time.textContent = formatTime(remaining);
    slot.blinds.innerHTML = blindsHTML(snap);
    slot.stats.innerHTML = statsHTML(snap);

    if (state === "break") {
      // Spec §7.1: during a break the whole slot shows the break, with the
      // remaining time in the same huge type as the playing levels.
      // The overlay repeats the level and the clock, so the ones behind
      // it are cleared: otherwise they would shine through the overlay
      // a second time.
      slot.level.textContent = "";
      slot.time.textContent = "";
      slot.blinds.innerHTML = "";
      slot.overlay.hidden = false;
      slot.overlayTitle.textContent = "BREAK";
      slot.overlayTime.textContent = formatTime(remaining);
      slot.overlaySub.textContent = level ? "Next: " + nextBlindsText(snap) : "";
    }
  }

  // setLevel writes the current level into the slot and into the break
  // overlay.
  //
  // The level index in the snapshot is 0-based, the display counts from 1.
  // When the tournament has no current level yet, nothing is shown: a
  // made-up number would be worse than no number.
  function setLevel(slot, level) {
    var text = "";
    if (level) {
      text = "LEVEL " + (level.index + 1);
      if (level.game) text += " — " + level.game;
    }
    slot.level.textContent = text;
    // During a break the current row is the break row itself, so the
    // overlay shows the number of that row.
    slot.overlayLevel.textContent = text;
  }

  // remainingSeconds computes the remaining time of the current level.
  //
  // While running, the end time of the level is the source of truth and the
  // countdown is computed locally. While paused, the frozen remainder of the
  // snapshot is shown. The countdown never goes below zero: the clock waits
  // for the event that moves the tournament on.
  function remainingSeconds(slot, snap) {
    if (snap.state === "paused") {
      return Math.max(0, snap.remaining_sec || 0);
    }
    if (snap.level_ends_at_ms === null || snap.level_ends_at_ms === undefined) {
      return 0;
    }
    var now = Date.now() + slot.offset;
    return Math.max(0, (snap.level_ends_at_ms - now) / 1000);
  }

  function formatTime(seconds) {
    var total = Math.max(0, Math.floor(seconds));
    var h = Math.floor(total / 3600);
    var m = Math.floor((total % 3600) / 60);
    var s = total % 60;
    if (h > 0) {
      return h + ":" + pad(m) + ":" + pad(s);
    }
    return pad(m) + ":" + pad(s);
  }

  function pad(n) {
    return n < 10 ? "0" + n : String(n);
  }

  // betsOf returns the forced bets of a level row.
  //
  // New snapshots carry the bets in one object; a snapshot of an older
  // server still has them flat on the row, so those are folded in here.
  function betsOf(level) {
    if (level.bets) return level.bets;
    return { sb: level.sb, bb: level.bb, ante: level.ante };
  }

  // betParts describes the forced bets of a level the way its format
  // asks for it (spec §7.7), as an array of "LABEL value" strings.
  //
  // The BIG BET is derived when the row has none: the BB times two for
  // a blind limit game, the COMPLETE times two for a stud game, and a
  // stud game shows it only when the game mix asks for it.
  function betParts(level, mix) {
    var bets = betsOf(level);
    var format = level.format || "blind-nl";
    var parts = [];
    if (format === "blind-limit") {
      pushBet(parts, "SB", bets.sb);
      pushBet(parts, "BB", bets.bb);
      pushBet(parts, "BIG BET", bigBetValue(bets, bets.bb));
      pushBet(parts, "ANTE", bets.ante);
    } else if (format === "stud-limit") {
      pushBet(parts, "BRING-IN", bets.bring_in);
      pushBet(parts, "COMPLETE", bets.complete);
      pushBet(parts, "ANTE", bets.ante);
      if (mix && mix.stud_show_big_bet) {
        pushBet(parts, "BIG BET", bigBetValue(bets, bets.complete));
      }
    } else {
      pushBet(parts, "SB", bets.sb);
      pushBet(parts, "BB", bets.bb);
      pushBet(parts, "ANTE", bets.ante);
    }
    return parts;
  }

  // bigBetValue returns the BIG BET of a row: its own value, or the
  // fallback (the BB or the COMPLETE) times two.
  function bigBetValue(bets, fallback) {
    if (bets.big_bet) return bets.big_bet;
    return (fallback || 0) * 2;
  }

  // pushBet adds a forced bet to the parts, unless it is not set.
  function pushBet(parts, label, value) {
    if (!value) return;
    parts.push(label + " " + fmt(value));
  }

  function blindsHTML(snap) {
    var level = snap.current_level;
    var parts = level ? betParts(level, snap.game_mix) : [];
    var html = parts.join(" &nbsp;·&nbsp; ");
    var next = nextBlindsText(snap);
    if (next) html += '<div class="next">Next: ' + next + "</div>";
    return html;
  }

  function nextBlindsText(snap) {
    var next = snap.next_level;
    if (!next) return "";
    if (next.type === "break") return "Break";
    var parts = betParts(next, snap.game_mix);
    if (next.game) parts.unshift(next.game);
    return parts.join(" / ");
  }

  // statsHTML builds the statistics block of a slot.
  //
  // The three primary numbers (players, entries, average stack) are
  // separate items with a label, so that they can be read at a glance
  // from a distance. The secondary numbers (rebuys, addons, reentries)
  // stay on a small line below and are only shown when there are any.
  // The values are the ones of the snapshot, nothing is computed here.
  function statsHTML(snap) {
    var items = [
      statItem("PLAYERS", fmt(snap.players_left || 0)),
      statItem("ENTRIES", fmt(snap.entries || 0)),
      statItem("AVG STACK", fmt(Math.round(snap.average_stack || 0)))
    ];
    var html = '<div class="stat-row">' + items.join("") + "</div>";
    var extra = [];
    if (snap.rebuys) extra.push("Rebuys " + fmt(snap.rebuys));
    if (snap.addons) extra.push("Addons " + fmt(snap.addons));
    if (snap.reentries) extra.push("Reentries " + fmt(snap.reentries));
    if (extra.length) {
      html += '<div class="stat-extra">' + extra.join(" &nbsp;·&nbsp; ") + "</div>";
    }
    return html;
  }

  function statItem(label, value) {
    return '<div class="stat"><span class="stat-label">' + label +
      '</span><span class="stat-value">' + value + "</span></div>";
  }

  function fmt(n) {
    return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  }

  // The local tick redraws every slot between two events.
  setInterval(function () {
    for (var i = 0; i < slots.length; i++) {
      render(slots[i]);
    }
  }, TICK_MS);

  // ---- fullscreen and wake lock ------------------------------------
  //
  // The display often runs on a phone or a tablet. The button gives the
  // page the whole screen, the wake lock keeps the screen on. Both are
  // best effort: a browser that refuses them must not break the page.

  var fullscreenButton = document.getElementById("fullscreen");
  var wakeLock = null;

  function inFullscreen() {
    return !!(document.fullscreenElement || document.webkitFullscreenElement);
  }

  function updateFullscreenButton() {
    if (!fullscreenButton) return;
    var on = inFullscreen();
    fullscreenButton.setAttribute("aria-pressed", on ? "true" : "false");
    fullscreenButton.setAttribute("aria-label", on ? "Exit fullscreen" : "Enter fullscreen");
    fullscreenButton.title = on ? "Exit fullscreen (Esc)" : "Enter fullscreen";
    fullscreenButton.textContent = on ? "⛶ EXIT FULLSCREEN" : "⛶ FULLSCREEN";
  }

  function toggleFullscreen() {
    try {
      if (inFullscreen()) {
        var exit = document.exitFullscreen || document.webkitExitFullscreen;
        if (!exit) return;
        var done = exit.call(document);
        if (done && typeof done.catch === "function") done.catch(function () {});
        return;
      }
      var root = document.documentElement;
      var request = root.requestFullscreen || root.webkitRequestFullscreen;
      if (!request) return;
      var started = request.call(root);
      if (started && typeof started.catch === "function") started.catch(function () {});
    } catch (err) {
      // A refused request is not an error for the display: the page
      // simply stays windowed.
    }
  }

  function requestWakeLock() {
    try {
      if (!navigator || !navigator.wakeLock) return;
      if (wakeLock && !wakeLock.released) return;
      navigator.wakeLock.request("screen").then(function (sentinel) {
        wakeLock = sentinel;
        if (sentinel && sentinel.addEventListener) {
          sentinel.addEventListener("release", function () { wakeLock = null; });
        }
      }).catch(function () {
        // Denied or unsupported: the display goes on without it.
      });
    } catch (err) {
      // Same as above: never let the wake lock break the page.
    }
  }

  if (fullscreenButton) {
    fullscreenButton.addEventListener("click", function () {
      toggleFullscreen();
      requestWakeLock();
    });
    document.addEventListener("fullscreenchange", updateFullscreenButton);
    document.addEventListener("webkitfullscreenchange", updateFullscreenButton);
    updateFullscreenButton();
  }

  // The first user gesture is the safest moment to ask for the wake
  // lock; the lock is released again when the tab is hidden, so it is
  // asked for anew when the page becomes visible again.
  document.addEventListener("click", requestWakeLock);
  document.addEventListener("keydown", requestWakeLock);
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "visible") requestWakeLock();
  });
  requestWakeLock();
})();
