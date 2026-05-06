import cors from "cors";
import csurf from "csurf";
import jwt from "jsonwebtoken";
import session from "express-session";
import path from "path";

function insecureCors(app, res) {
  // ruleid: javascript-cors-wildcard-credentials
  app.use(cors({ origin: "*", credentials: true }));
  // ruleid: javascript-cors-wildcard-credentials
  res.set("Access-Control-Allow-Origin", "*");
  res.set("Access-Control-Allow-Credentials", "true");
}

function safeCors(app) {
  // ok: javascript-cors-wildcard-credentials
  app.use(cors({ origin: ["https://example.com"], credentials: true }));
}

function insecureCsrf(app) {
  // ruleid: javascript-csrf-csurf-state-changing-ignore
  app.use(csurf({ ignoreMethods: ["GET", "HEAD", "OPTIONS", "POST"] }));
}

function safeCsrf(app) {
  // ok: javascript-csrf-csurf-state-changing-ignore
  app.use(csurf({ ignoreMethods: ["GET", "HEAD", "OPTIONS"] }));
}

function insecureSecrets(app, token, payload) {
  // ruleid: javascript-hardcoded-web-secret
  jwt.sign(payload, "super-secret");
  // ruleid: javascript-hardcoded-web-secret
  app.use(session({ secret: "keyboard-cat" }));
}

function safeSecrets(token, payload) {
  // ok: javascript-hardcoded-web-secret
  jwt.sign(payload, process.env.JWT_SECRET);
}

function uploadName(file, cb, baseDir) {
  // ruleid: javascript-upload-originalname-path
  cb(null, file.originalname);
  // ruleid: javascript-upload-originalname-path
  return path.join(baseDir, file.originalname);
}

function safeUploadName(file, cb, baseDir) {
  // ok: javascript-upload-originalname-path
  cb(null, path.basename(file.originalname));
}
