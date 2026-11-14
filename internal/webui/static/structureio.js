// PonyTimer — structure import and export helpers (issue #6, step D).
//
// The helpers are pure functions: they read text and give text or plain
// objects back, and they never touch the DOM. The same file is used by
// the editor page (window.PonyIO) and by the check harness
// (module.exports), so the parsing rules can be checked without a
// browser.
(function (global) {
  "use strict";

  // CSV_HEADER is the column list of an exported structure file.
  var CSV_HEADER = ["type", "game", "format", "sb", "bb", "big_bet", "ante",
    "bring_in", "complete", "duration_min", "markers"];

  // DEFAULT_DURATION_MIN is the length of a row that does not carry one.
  var DEFAULT_DURATION_MIN = 20;

  // BET_KEYS are the forced bet fields of a level row.
  var BET_KEYS = ["sb", "bb", "big_bet", "ante", "bring_in", "complete"];

  // isNumber reports whether the token is a plain non negative number.
  function isNumber(token) {
    return /^\d+(\.\d+)?$/.test(token);
  }

  // splitFields splits one table line into its fields.
  //
  // The separators are the tab, the comma and the space, and they may be
  // mixed. The empty fields are dropped, so "100, 200" and "100 200" are
  // the same line.
  function splitFields(line) {
    return line.split(/[\t,\s]+/).filter(function (field) {
      return field !== "";
    });
  }

  // blankBets returns a bets object with every field set to zero.
  function blankBets() {
    return { sb: 0, bb: 0, big_bet: 0, ante: 0, bring_in: 0, complete: 0 };
  }

  // makeLevel builds a no limit level row.
  function makeLevel(sb, bb, ante, durationSec) {
    var bets = blankBets();
    bets.sb = sb;
    bets.bb = bb;
    bets.ante = ante;
    return {
      type: "level",
      format: "blind-nl",
      game: "",
      bets: bets,
      duration_sec: durationSec,
      markers: []
    };
  }

  // makeBreak builds a break row.
  function makeBreak(durationSec) {
    return {
      type: "break",
      format: "",
      game: "",
      bets: {},
      duration_sec: durationSec,
      markers: []
    };
  }

  // parseTableText reads a pasted structure table.
  //
  // The fields of a line are separated by tabs, commas or spaces (mixed is
  // fine). The empty lines and the lines starting with "#" are skipped. A
  // line of numbers is a blind level: two columns are SB and BB, three
  // columns add the ante and four columns add the length in minutes. A
  // line starting with "break" is a break row and the number after it is
  // its length.
  //
  // The options are:
  //
  //   durationMin:  the length of a row without one (20 minutes by default)
  //   anteEqualsBB: true to fill the ante of a two column row with the BB
  //
  // The rows are returned in the level JSON of the API, without their
  // index: the caller numbers them.
  function parseTableText(text, opts) {
    opts = opts || {};
    var durationMin = Number(opts.durationMin);
    if (!isFinite(durationMin) || durationMin <= 0) {
      durationMin = DEFAULT_DURATION_MIN;
    }
    var anteEqualsBB = !!opts.anteEqualsBB;
    var levels = [];
    var lines = String(text == null ? "" : text).split(/\r?\n/);
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line.charAt(0) === "#") continue;
      var fields = splitFields(line);
      var lineNo = i + 1;
      if (!fields.length) continue;

      if (fields[0].toLowerCase() === "break") {
        if (fields.length > 2) {
          throw new Error("line " + lineNo +
            ": a break line takes one number of minutes at most: \"" + line + "\"");
        }
        var breakMin = durationMin;
        if (fields.length === 2) {
          if (!isNumber(fields[1])) {
            throw new Error("line " + lineNo +
              ": the length of the break must be a number of minutes: \"" + line + "\"");
          }
          breakMin = parseFloat(fields[1]);
        }
        levels.push(makeBreak(Math.round(breakMin * 60)));
        continue;
      }

      if (fields.length < 2 || fields.length > 4) {
        throw new Error("line " + lineNo +
          ": expected SB, BB[, Ante[, minutes]]: \"" + line + "\"");
      }
      var numbers = [];
      for (var n = 0; n < fields.length; n++) {
        if (!isNumber(fields[n])) {
          throw new Error("line " + lineNo +
            ": the forced bets must be numbers: \"" + line + "\"");
        }
        numbers.push(parseFloat(fields[n]));
      }
      var sb = numbers[0];
      var bb = numbers[1];
      var ante = numbers.length >= 3 ? numbers[2] : (anteEqualsBB ? bb : 0);
      if (sb !== Math.round(sb) || bb !== Math.round(bb) || ante !== Math.round(ante)) {
        throw new Error("line " + lineNo +
          ": the forced bets must be whole numbers: \"" + line + "\"");
      }
      var minutes = numbers.length === 4 ? numbers[3] : durationMin;
      levels.push(makeLevel(sb, bb, ante, Math.round(minutes * 60)));
    }
    return levels;
  }

  // csvField writes one CSV field, quoting it when it needs it.
  function csvField(value) {
    var text = value == null ? "" : String(value);
    if (/[",\n\r]/.test(text) || text !== text.trim()) {
      return "\"" + text.replace(/"/g, "\"\"") + "\"";
    }
    return text;
  }

  // minutesText writes a duration in minutes without a trailing zero.
  function minutesText(seconds) {
    var minutes = (Number(seconds) || 0) / 60;
    if (minutes === Math.round(minutes)) return String(Math.round(minutes));
    return String(Math.round(minutes * 1e6) / 1e6);
  }

  // levelsToCSV writes the structure as a CSV file.
  //
  // The header is the one of CSV_HEADER and the markers of a row are
  // joined with a semicolon.
  function levelsToCSV(levels) {
    var lines = [CSV_HEADER.join(",")];
    (levels || []).forEach(function (level) {
      var row = level || {};
      var bets = row.bets || {};
      var isBreak = row.type === "break";
      lines.push([
        row.type || "level",
        row.game || "",
        isBreak ? "" : (row.format || "blind-nl"),
        Number(bets.sb) || 0,
        Number(bets.bb) || 0,
        Number(bets.big_bet) || 0,
        Number(bets.ante) || 0,
        Number(bets.bring_in) || 0,
        Number(bets.complete) || 0,
        minutesText(row.duration_sec),
        (row.markers || []).join(";")
      ].map(csvField).join(","));
    });
    return lines.join("\n") + "\n";
  }

  // parseCSVRows splits a CSV text into rows of fields.
  //
  // The quotes are honoured: a quoted field may hold a comma, a quote or
  // a line break.
  function parseCSVRows(text) {
    var rows = [];
    var row = [];
    var field = "";
    var inQuotes = false;
    var source = String(text == null ? "" : text);
    var i = 0;
    while (i < source.length) {
      var ch = source.charAt(i);
      if (inQuotes) {
        if (ch === '"') {
          if (source.charAt(i + 1) === '"') {
            field += '"';
            i += 2;
            continue;
          }
          inQuotes = false;
          i++;
          continue;
        }
        field += ch;
        i++;
        continue;
      }
      if (ch === '"') {
        inQuotes = true;
        i++;
        continue;
      }
      if (ch === ",") {
        row.push(field);
        field = "";
        i++;
        continue;
      }
      if (ch === "\r") {
        i++;
        continue;
      }
      if (ch === "\n") {
        row.push(field);
        rows.push(row);
        row = [];
        field = "";
        i++;
        continue;
      }
      field += ch;
      i++;
    }
    if (field !== "" || row.length) {
      row.push(field);
      rows.push(row);
    }
    return rows;
  }

  // isEmptyRow reports whether every field of the row is blank.
  function isEmptyRow(row) {
    return row.every(function (field) {
      return String(field).trim() === "";
    });
  }

  // csvValue reads one column of a row.
  function csvValue(row, index, name) {
    if (!(name in index)) return "";
    var at = index[name];
    return at < row.length ? String(row[at]).trim() : "";
  }

  // parseCSV reads a structure written by levelsToCSV.
  //
  // The header row is required: it names the columns. The rows that are
  // not empty are read in order and returned in the level JSON of the
  // API, without their index.
  function parseCSV(text) {
    var rows = parseCSVRows(text).filter(function (row) {
      return !isEmptyRow(row);
    });
    if (!rows.length) {
      throw new Error("the CSV file is empty");
    }
    var index = {};
    rows[0].forEach(function (name, at) {
      var key = String(name).trim().toLowerCase();
      if (key && !(key in index)) index[key] = at;
    });
    if (!("type" in index) || !("duration_min" in index)) {
      throw new Error("the CSV header must have the type and duration_min columns");
    }

    var levels = [];
    for (var r = 1; r < rows.length; r++) {
      var row = rows[r];
      var lineNo = r + 1;
      var type = csvValue(row, index, "type").toLowerCase();
      if (type !== "level" && type !== "break") {
        throw new Error("line " + lineNo + ": the type must be level or break");
      }
      var minutes = csvValue(row, index, "duration_min");
      if (!isNumber(minutes)) {
        throw new Error("line " + lineNo +
          ": the duration must be a number of minutes");
      }
      var duration = Math.round(parseFloat(minutes) * 60);
      var markers = csvValue(row, index, "markers").split(";").map(function (marker) {
        return marker.trim();
      }).filter(function (marker) {
        return marker !== "";
      });
      if (type === "break") {
        var breakRow = makeBreak(duration);
        breakRow.markers = markers;
        levels.push(breakRow);
        continue;
      }
      var bets = blankBets();
      for (var b = 0; b < BET_KEYS.length; b++) {
        var key = BET_KEYS[b];
        var raw = csvValue(row, index, key);
        if (!raw) continue;
        if (!/^\d+$/.test(raw)) {
          throw new Error("line " + lineNo + ": " + key + " must be a whole number");
        }
        bets[key] = parseInt(raw, 10);
      }
      var levelMarkers = markers;
      levels.push({
        type: "level",
        format: csvValue(row, index, "format") || "blind-nl",
        game: csvValue(row, index, "game"),
        bets: bets,
        duration_sec: duration,
        markers: levelMarkers
      });
    }
    return levels;
  }

  var PonyIO = {
    parseTableText: parseTableText,
    levelsToCSV: levelsToCSV,
    parseCSV: parseCSV
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = PonyIO;
  }
  if (typeof window !== "undefined") {
    window.PonyIO = PonyIO;
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
