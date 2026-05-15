// ruleid: javascript-misc-sensitive-data-in-log
function logPassword(password) {
    console.log("User password:", password);
}

// ruleid: javascript-misc-sensitive-data-in-log
function logToken(accessToken) {
    console.debug("Token: " + accessToken);
}

// ruleid: javascript-misc-sensitive-data-in-log
function logSecret(secretKey) {
    console.error(`Key = ${secretKey}`);
}

// ruleid: javascript-misc-sensitive-data-in-log
function logApiKey(apiKey) {
    console.warn("API key:", apiKey);
}

// ok: javascript-misc-sensitive-data-in-log
function logNormal(username) {
    console.log("User logged in:", username);
}

// ok: javascript-misc-sensitive-data-in-log
function logMessage() {
    console.log("Please enter your password");
}

// ok: javascript-misc-sensitive-data-in-log
function logUserId(userId) {
    console.debug("Processing user:", userId);
}
