// JavaScript crypto 弱算法 / 不安全随机 / TLS 跳过验证 测试用例
// 仅用于 semgrep 规则验证

const crypto = require("crypto");
const https = require("https");

// ============= 弱算法 =============

function useDES(key) {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createCipher("des", key);
}

function use3DES(key, iv) {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createCipheriv("des-ede3-cbc", key, iv);
}

function useRC4(key) {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createCipher("rc4", key);
}

function useMD5() {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createHash("md5");
}

function useSHA1() {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createHash("sha1");
}

function useHmacMD5(key) {
    // ruleid: js-crypto-weak-algorithm
    return crypto.createHmac("md5", key);
}

// ============= 不安全随机 =============

function generateToken() {
    // ruleid: js-crypto-insecure-random
    return Math.random().toString(36).substring(2);
}

function generateSessionId() {
    // ruleid: js-crypto-insecure-random
    return "sess_" + Math.random();
}

// ============= TLS 跳过验证 =============

function insecureRequest(url) {
    // ruleid: js-crypto-tls-reject-unauthorized-false
    const agent = new https.Agent({ rejectUnauthorized: false });
    return fetch(url, { agent });
}

function disableGlobal() {
    // ruleid: js-crypto-tls-reject-unauthorized-false
    process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
}

// ============= 安全写法 =============

function safeCipher(key, iv) {
    // ok: js-crypto-weak-algorithm
    return crypto.createCipheriv("aes-256-gcm", key, iv);
}

function safeHash() {
    // ok: js-crypto-weak-algorithm
    return crypto.createHash("sha256");
}

function safeRandom() {
    // ok: js-crypto-insecure-random
    return crypto.randomBytes(32).toString("hex");
}
