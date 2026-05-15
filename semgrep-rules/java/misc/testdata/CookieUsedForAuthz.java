import javax.servlet.http.Cookie;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpSession;
import org.springframework.web.bind.annotation.CookieValue;

class CookieUsedForAuthz {
    void insecure(HttpServletRequest request, String account) {
        String adminID = "";
        Cookie[] cookies = request.getCookies();
        if (cookies != null) {
            for (Cookie cookie : cookies) {
                if (cookie.getName().equals("adminID")) {
                    adminID = cookie.getValue();
                }
            }
        }
        // ruleid: java-misc-cookie-used-for-authz
        if (account.equals(adminID)) {
            performSensitiveOp();
        }
    }

    // ruleid: java-misc-cookie-used-for-authz
    void insecureCookieValue(@CookieValue("role") String role) {
        if (role.equals("admin")) {
            performSensitiveOp();
        }
    }

    void safe(HttpServletRequest request, String account) {
        HttpSession session = request.getSession();
        String adminID = (String) session.getAttribute("adminID");
        // ok: java-misc-cookie-used-for-authz
        if (account.equals(adminID)) {
            performSensitiveOp();
        }
    }

    // ok: java-misc-cookie-used-for-authz
    void safeCookieValue(@CookieValue("theme") String theme) {
        if (theme.equals("dark")) {
            applyTheme();
        }
    }

    private void performSensitiveOp() {}
    private void applyTheme() {}
}
