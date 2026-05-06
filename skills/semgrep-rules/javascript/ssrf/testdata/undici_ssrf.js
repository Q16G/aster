import { request } from "undici";

function insecureNamespace(url) {
  // ruleid: javascript-ssrf-undici
  return undici.request(url);
}

function insecureImported(url) {
  // ruleid: javascript-ssrf-undici
  return request(url);
}

function safe(url) {
  // ok: javascript-ssrf-undici
  return undici.fetch("https://api.example.com/health");
}
