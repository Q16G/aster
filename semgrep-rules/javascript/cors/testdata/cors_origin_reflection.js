const express = require("express");

// ruleid: javascript-cors-origin-reflection
function reflectOriginInline(req, res) {
    res.set("Access-Control-Allow-Origin", req.headers.origin);
}

// ruleid: javascript-cors-origin-reflection
function reflectOriginVariable(req, res) {
    const origin = req.headers.origin;
    res.set("Access-Control-Allow-Origin", origin);
}

// ok: javascript-cors-origin-reflection
function staticOrigin(req, res) {
    res.set("Access-Control-Allow-Origin", "https://trusted.example.com");
}

// ok: javascript-cors-origin-reflection
function wildcardOrigin(req, res) {
    res.set("Access-Control-Allow-Origin", "*");
}
