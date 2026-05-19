<?php
// PHP 综合漏洞测试用例
// 仅用于 semgrep 规则验证

// ============= deserialization =============

// ruleid: php-deserialization-unserialize-user-input
$data = $_GET['data'];
unserialize($data);

// ruleid: php-deserialization-phar-deserialization
$path = $_GET['file'];
file_get_contents($path);

// ============= injection: command =============

// ruleid: php-injection-command-injection-exec
$cmd = $_GET['cmd'];
exec($cmd);

// ruleid: php-injection-command-injection-exec
$host = $_POST['host'];
system("ping " . $host);

// ruleid: php-injection-command-injection-backtick
$ip = $_GET['ip'];
shell_exec($ip);

// ============= injection: ldap =============

// ruleid: php-injection-ldap-injection
$user = $_GET['user'];
$conn = ldap_connect("ldap://localhost");
ldap_search($conn, "dc=example,dc=com", $user);

// ============= injection: sql =============

// ruleid: php-injection-sql-injection-pdo
$id = $_GET['id'];
$pdo->query($id);

// ============= misc: file upload =============

// ruleid: php-misc-file-upload-no-check
move_uploaded_file($_FILES['f']['tmp_name'], "/uploads/" . $_FILES['f']['name']);

// ============= misc: open redirect =============

// ruleid: php-misc-open-redirect
$url = $_GET['url'];
header("Location: " . $url);

// ============= misc: path traversal =============

// ruleid: php-misc-path-traversal
$page = $_GET['page'];
include($page);

// ============= ssrf =============

// ruleid: php-ssrf-curl
$target = $_GET['url'];
$ch = curl_init();
curl_setopt($ch, CURLOPT_URL, $target);

// ruleid: php-ssrf-file-get-contents
$remote = $_POST['url'];
file_get_contents($remote);

// ============= ssti =============

// ruleid: php-ssti-twig
$input = $_GET['template'];
$twig->createTemplate($input);

// ============= xss =============

// ruleid: php-xss-echo-print
$name = $_GET['name'];
echo $name;

// ruleid: php-xss-unescaped-output
$msg = $_POST['msg'];
echo $msg;

// ruleid: php-xss-blade-unescaped
$comment = $request->input('comment');
echo $comment;

// ============= xxe =============

// ruleid: php-xxe-dom
$xml = file_get_contents("php://input");
$doc = new DOMDocument();
$doc->loadXML($xml);

// ruleid: php-xxe-simplexml
$xmlData = $_POST['xml'];
simplexml_load_string($xmlData);

// ============= 安全写法（不应被命中） =============

// ok: php-xss-echo-print
$safe = htmlspecialchars($_GET['name'], ENT_QUOTES, 'UTF-8');
echo $safe;

// ok: php-injection-command-injection-exec
$safeCmd = escapeshellarg($_GET['arg']);
exec("ls " . $safeCmd);

// ok: php-misc-path-traversal
$safePath = basename($_GET['file']);
include($safePath);

// ok: php-injection-sql-injection-pdo
$stmt = $pdo->prepare("SELECT * FROM users WHERE id = ?");
$stmt->execute([$_GET['id']]);
