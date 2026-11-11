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
      '<div class="slot-time"></div>' +
      '<div class="slot-message"></div>' +
      '<div class="slot-blinds"></div>' +
      '<div class="slot-stats"></div>' +
      '<div class="overlay" hidden>' +
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
      time: el.querySelector(".slot-time"),
      message: el.querySelector(".slot-message"),
      blinds: el.querySelector(".slot-blinds"),
      stats: el.querySelector(".slot-stats"),
      overlay: el.querySelector(".overlay"),
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

    var state = snap.state;
    var remaining = remainingSeconds(slot, snap);

    slot.message.textContent = "";
    slot.overlay.hidden = true;

    if (state === "waiting") {
      slot.time.textContent = "";
      slot.message.textContent = "Waiting To Begin";
      slot.blinds.innerHTML = "";
      slot.stats.textContent = "";
      return;
    }
    if (state === "finished") {
      slot.time.textContent = "";
      slot.message.textContent = "Finished";
      slot.blinds.innerHTML = "";
      slot.stats.textContent = "";
      return;
    }

    slot.time.textContent = formatTime(remaining);
    slot.blinds.innerHTML = blindsHTML(snap);
    slot.stats.innerHTML = statsText(snap);

    if (state === "break") {
      // Spec §7.1: during a break the whole slot shows the break, with the
      // remaining time in the same huge type as the playing levels.
      slot.overlay.hidden = false;
      slot.overlayTitle.textContent = "BREAK";
      slot.overlayTime.textContent = formatTime(remaining);
      slot.overlaySub.textContent = level ? "Next: " + nextBlindsText(snap) : "";
    }
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

  function blindsHTML(snap) {
    var level = snap.current_level;
    var parts = [];
    if (level) {
      parts.push("SB " + fmt(level.sb));
      parts.push("BB " + fmt(level.bb));
      if (level.ante) parts.push("ANTE " + fmt(level.ante));
    }
    var html = parts.join(" &nbsp;·&nbsp; ");
    var next = nextBlindsText(snap);
    if (next) html += '<div class="next">Next: ' + next + "</div>";
    return html;
  }

  function nextBlindsText(snap) {
    var next = snap.next_level;
    if (!next) return "";
    if (next.type === "break") return "Break";
    var parts = ["SB " + fmt(next.sb), "BB " + fmt(next.bb)];
    if (next.ante) parts.push("ANTE " + fmt(next.ante));
    return parts.join(" / ");
  }

  function statsText(snap) {
    var parts = [];
    parts.push("Players " + snap.players_left + " / " + snap.entries);
    if (snap.rebuys) parts.push("Rebuys " + snap.rebuys);
    if (snap.addons) parts.push("Addons " + snap.addons);
    if (snap.reentries) parts.push("Reentries " + snap.reentries);
    parts.push("Avg stack " + fmt(Math.round(snap.average_stack || 0)));
    return parts.join(" &nbsp;·&nbsp; ");
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
})();
