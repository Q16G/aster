<?php
// PHP ReDoS / 用户控制的正则模式 测试用例
// 仅用于 semgrep 规则验证

// ============= 超全局变量 =============

function fromGet() {
    // ruleid: php-redos-tainted-pattern
    return preg_match($_GET['pat'], 'abc');
}

function fromPost() {
    // ruleid: php-redos-tainted-pattern
    preg_match_all($_POST['pat'], 'data', $m);
    return $m;
}

function fromRequest() {
    // ruleid: php-redos-tainted-pattern
    return preg_replace($_REQUEST['pat'], 'X', 'data');
}

function fromCookie() {
    // ruleid: php-redos-tainted-pattern
    return preg_split($_COOKIE['sep'], 'a,b,c');
}

// ============= Laravel =============

function fromLaravelRequest($request) {
    // ruleid: php-redos-tainted-pattern
    return preg_match($request->input('pat'), 'abc');
}

function fromLaravelHelper() {
    // ruleid: php-redos-tainted-pattern
    return preg_match(request('pat'), 'abc');
}

// ============= mb_ereg =============

function fromGetMb() {
    // ruleid: php-redos-tainted-pattern
    return mb_ereg($_GET['pat'], 'abc');
}

// ============= 安全写法（不应被命中） =============

function literalPattern() {
    // ok: php-redos-tainted-pattern
    return preg_match('/^[a-zA-Z0-9_-]+$/', 'abc');
}

function literalSplit() {
    // ok: php-redos-tainted-pattern
    return preg_split('/,/', 'a,b,c');
}

function fixedPatternFromConst() {
    $pat = '/^\d+$/';
    // ok: php-redos-tainted-pattern  ← 实际命中（变量但非用户输入），
    // 由于 metavariable-pattern 限定来源为 $_GET 等，不会命中。
    return preg_match($pat, '123');
}
