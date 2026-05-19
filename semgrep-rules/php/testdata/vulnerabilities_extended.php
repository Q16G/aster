<?php
// PHP 扩展漏洞测试用例（hardcoded / crypto / session / type-juggling）
// 仅用于 semgrep 规则验证

// ============= hardcoded secrets =============

// ruleid: php-hardcoded-password
$password = "SuperSecret123";

// ruleid: php-hardcoded-password
$api_key = "sk-1234567890abcdef";

// ruleid: php-hardcoded-password
$jwt_secret = "my-jwt-signing-key";

// ruleid: php-hardcoded-db-credentials
$pdo = new PDO("mysql:host=localhost;dbname=app", "root", "p@ssw0rd");

// ruleid: php-hardcoded-db-credentials
$conn = new mysqli("localhost", "admin", "secret123", "mydb");

// ============= crypto: weak hash =============

// ruleid: php-crypto-weak-hash-for-password
$hash = md5($userPassword);

// ruleid: php-crypto-weak-hash-for-password
$hash2 = sha1($userPassword);

// ruleid: php-crypto-weak-hash-for-password
$hash3 = hash("md5", $userPassword);

// ============= crypto: insecure random =============

// ruleid: php-crypto-insecure-random
$token = md5(uniqid());

// ruleid: php-crypto-insecure-random
$code = rand(100000, 999999);

// ruleid: php-crypto-insecure-random
$sessionId = mt_rand();

// ============= crypto: ECB mode =============

// ruleid: php-crypto-ecb-mode
$encrypted = openssl_encrypt($data, "aes-256-ecb", $key);

// ============= session fixation =============

// ruleid: php-session-fixation-no-regenerate
session_start();
$_SESSION['user'] = $username;

// ruleid: php-session-id-from-user-input
$sid = $_GET['sid'];
session_id($sid);

// ============= type juggling =============

// ruleid: php-type-juggling-loose-comparison
if (md5($input) == $storedHash) {
    echo "authenticated";
}

// ruleid: php-type-juggling-loose-comparison
if ($token == hash("sha256", $secret)) {
    echo "valid";
}

// ruleid: php-type-juggling-strcmp-bypass
$pass = $_POST['password'];
if (strcmp($pass, $correctPassword) == 0) {
    echo "login ok";
}

// ============= 修复后的 sql-injection-raw (taint) =============

// ruleid: php-injection-sql-injection-raw
$id = $_GET['id'];
mysqli_query($conn, "SELECT * FROM users WHERE id = " . $id);

// ruleid: php-sql-injection-mysqli
$name = $_POST['name'];
$mysqli->query("SELECT * FROM users WHERE name = '" . $name . "'");

// ============= 修复后的 LFI (taint) =============

// ruleid: php-lfi-include
$page = $_GET['page'];
include($page);

// ruleid: php-lfi-wrapper
$file = $_GET['file'];
file_get_contents($file);

// ============= 安全写法（不应被命中） =============

// ok: php-hardcoded-password
$password = "";

// ok: php-crypto-weak-hash-for-password
$safe_hash = password_hash($userPassword, PASSWORD_BCRYPT);

// ok: php-crypto-insecure-random
$safe_token = bin2hex(random_bytes(32));

// ok: php-type-juggling-loose-comparison
if (hash_equals($storedHash, md5($input))) {
    echo "safe";
}

// ok: php-session-fixation-no-regenerate
session_start();
session_regenerate_id(true);
$_SESSION['user'] = $username;

// ok: php-lfi-include
$safePage = basename($_GET['page']);
include($safePage);

// ok: php-injection-sql-injection-raw
$safeId = intval($_GET['id']);
mysqli_query($conn, "SELECT * FROM users WHERE id = " . $safeId);
