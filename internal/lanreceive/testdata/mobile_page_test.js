"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const {File} = require("node:buffer");
const {webcrypto} = require("node:crypto");

// Exercise the shipped script with only its DOM and network boundaries faked.
// Files retain real byte content, and FormData carries those same files through
// failed requests and retries.
class Element {
  constructor() {
    this.style = {};
    this.dataset = {};
    this.children = [];
  }
  setAttribute() {}
  addEventListener() {}
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children = children; }
  remove() {}
}

const requests = [], responses = [];
class XMLHttpRequest {
  constructor() { this.headers = {}; this.upload = {}; }
  open() {}
  setRequestHeader(name, value) { this.headers[name] = value; }
  send(body) {
    requests.push({headers: this.headers, file: body.get("image")});
    const response = responses.shift();
    assert.ok(response, "unexpected upload request");
    queueMicrotask(() => {
      if (response.networkError) { this.onerror(); return; }
      this.status = response.status;
      this.responseText = JSON.stringify(response.body);
      this.onload();
    });
  }
}

async function main() {
  const html = fs.readFileSync(path.join(__dirname, "..", "mobile.html"), "utf8");
  let script = html.match(/<script>([\s\S]*?)<\/script>/)[1];
  // Expose closure-owned operations without changing their shipped bodies.
  script = script.replace(/\}\)\(\);\s*$/, "globalThis.review = {items, selectFiles, sendItems}; })();");
  const nodes = new Map();
  const now = new Date();
  const context = {
    crypto: webcrypto, performance, FormData, XMLHttpRequest,
    location: {hash: "#test-session-token"},
    navigator: {language: "en", onLine: true},
    URL: {createObjectURL() { return "blob:test-preview"; }, revokeObjectURL() {}},
    document: {
      documentElement: {},
      getElementById(id) {
        if (!nodes.has(id)) nodes.set(id, new Element());
        return nodes.get(id);
      },
      querySelectorAll() { return []; },
      createElement() { return new Element(); },
      addEventListener() {},
    },
    window: {addEventListener() {}},
    fetch: async () => ({
      ok: true, status: 200,
      json: async () => ({target: "upload", language: "en", maxBytes: 100 << 20,
        maxPixels: 40_000_000, serverNow: now.toISOString(),
        expiresAt: new Date(+now + 15 * 60_000).toISOString()}),
    }),
    AbortController, setTimeout, clearTimeout, setInterval() {},
  };
  vm.createContext(context);
  vm.runInContext(script, context);
  await new Promise(setImmediate); // Complete the normal session request.
  assert.equal(nodes.get("choose").disabled, false, "session did not become ready");

  const first = new File(["AAAA"], "image.png", {type: "image/png", lastModified: 1234});
  const second = new File(["BBBB"], "image.png", {type: "image/png", lastModified: 1234});
  const page = context.review;
  page.selectFiles([first, second]);
  assert.equal(page.items.length, 2, "metadata collision silently discarded a distinct photo");
  assert.equal(await page.items[0].file.text(), "AAAA");
  assert.equal(await page.items[1].file.text(), "BBBB");

  // A deliberate second selection is a new item, while retrying an existing
  // item retains its original identity and source until acknowledged.
  page.selectFiles([first]);
  assert.equal(page.items.length, 3, "explicit selection was silently ignored");
  assert.notEqual(page.items[0].id, page.items[2].id);
  const item = page.items[0];
  responses.push({networkError: true});
  await page.sendItems([item]);
  assert.equal(item.state, "error");
  assert.equal(item.file, first, "failed upload released its source");

  responses.push({status: 200, body: {name: "image.png", size: first.size}});
  await page.sendItems([item]);
  assert.equal(item.state, "success");
  assert.equal(item.file, null, "successful upload retained its file bytes");
  assert.equal(requests.length, 2);
  assert.equal(requests[0].headers["X-Upload-ID"], requests[1].headers["X-Upload-ID"],
    "retry changed the server idempotency key");
  assert.equal(await requests[0].file.text(), "AAAA");
  assert.equal(await requests[1].file.text(), "AAAA");
}

main().catch(error => { console.error(error); process.exitCode = 1; });
