// JavaScript JWT 测试用例（algorithm-confusion / claim 校验缺失 / 弱 secret）
// 仅用于 semgrep 规则验证

const jwt = require("jsonwebtoken");
const fs = require("fs");

const publicKey = fs.readFileSync("public.pem");

// ============= algorithm confusion: 不传 algorithms =============

function verifyNoAlgs(token) {
    // ruleid: js-jwt-algorithm-confusion-no-algorithms
    return jwt.verify(token, publicKey);
}

function verifyNoAlgsCallback(token, cb) {
    // ruleid: js-jwt-algorithm-confusion-no-algorithms
    jwt.verify(token, publicKey, cb);
}

// ============= 显式接受 alg=none =============

function verifyAllowsNone(token) {
    // ruleid: js-jwt-algorithms-allows-none
    return jwt.verify(token, "", { algorithms: ["none"] });
}

function verifyMixesNone(token) {
    // ruleid: js-jwt-algorithms-allows-none
    return jwt.verify(token, publicKey, { algorithms: ["RS256", "none"] });
}

// ============= missing claim validation =============

function verifyOnlyAlgs(token) {
    // ruleid: js-jwt-missing-claim-validation
    return jwt.verify(token, publicKey, { algorithms: ["RS256"] });
}

// ============= 弱 secret =============

function signShort(payload) {
    // ruleid: js-jwt-weak-secret
    return jwt.sign(payload, "secret");
}

function signShort2(payload) {
    // ruleid: js-jwt-weak-secret
    return jwt.sign(payload, "my-app-key");
}

function verifyShort(token) {
    // ruleid: js-jwt-weak-secret
    return jwt.verify(token, "abc123", { algorithms: ["HS256"] });
}

// ============= 安全写法（不应被命中） =============

function verifySafe(token) {
    // ok: js-jwt-algorithm-confusion-no-algorithms
    // ok: js-jwt-missing-claim-validation
    return jwt.verify(token, publicKey, {
        algorithms: ["RS256"],
        issuer: "https://issuer.example.com",
        audience: "my-api",
    });
}

const longSecret = "a-very-long-randomly-generated-secret-key-of-32-plus-chars";
function signSafe(payload) {
    // ok: js-jwt-weak-secret
    return jwt.sign(payload, longSecret);
}
