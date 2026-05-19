// Java ReDoS 测试用例
// 仅用于 semgrep 规则验证
package testdata;

import java.util.regex.Matcher;
import java.util.regex.Pattern;

import javax.servlet.http.HttpServletRequest;

import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class Redos {

    // ============= Servlet =============

    public boolean fromParam(HttpServletRequest req) {
        String p = req.getParameter("pat");
        // ruleid: java-redos-tainted-pattern
        Pattern.compile(p);
        return true;
    }

    public boolean fromHeader(HttpServletRequest req) {
        String p = req.getHeader("X-Pattern");
        // ruleid: java-redos-tainted-pattern
        return Pattern.matches(p, "abc");
    }

    public String fromBody(HttpServletRequest req) {
        String p = req.getParameter("pat");
        String input = "data";
        // ruleid: java-redos-tainted-pattern
        return input.replaceAll(p, "X");
    }

    // ============= Spring =============

    @GetMapping("/match")
    public boolean springParam(@RequestParam("pat") String pat) {
        // ruleid: java-redos-tainted-pattern
        Pattern p = Pattern.compile(pat);
        Matcher m = p.matcher("abc");
        return m.find();
    }

    @GetMapping("/split")
    public String[] springSplit(@RequestParam("sep") String sep) {
        String input = "a,b,c";
        // ruleid: java-redos-tainted-pattern
        return input.split(sep);
    }

    // ============= 安全写法（不应被命中） =============

    private static final Pattern STATIC_RE = Pattern.compile("^[a-zA-Z0-9_-]+$");

    @GetMapping("/static")
    public boolean staticPattern(@RequestParam("v") String v) {
        // ok: java-redos-tainted-pattern
        return STATIC_RE.matcher(v).matches();
    }

    @GetMapping("/quote")
    public boolean quoted(@RequestParam("v") String v) {
        // ok: java-redos-tainted-pattern
        return Pattern.compile(Pattern.quote(v)).matcher("abc").find();
    }
}
