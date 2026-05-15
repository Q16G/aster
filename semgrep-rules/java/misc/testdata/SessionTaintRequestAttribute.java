import java.util.Map;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpSession;

class SessionTaintRequestAttribute {
    void insecureDirect(HttpServletRequest request) {
        Map<String, Object> payload = (Map<String, Object>) request.getAttribute("payload");
        String sessionValue = (String) payload.get("principal");
        // ruleid: java-misc-session-taint-request-attribute
        request.getSession().setAttribute("principal", sessionValue);
    }

    void insecureAlias(HttpServletRequest request) {
        Map<String, Object> payload = (Map<String, Object>) request.getAttribute("payload");
        String sessionValue = (String) payload.get("principal");
        HttpSession session = request.getSession();
        // ruleid: java-misc-session-taint-alias-sink
        session.setAttribute("principal", sessionValue);
    }

    void safeConstant(HttpServletRequest request) {
        HttpSession session = request.getSession();
        // ok: java-misc-session-taint-request-attribute
        session.setAttribute("role", "doctor");
        // ok: java-misc-session-taint-alias-sink
        session.setAttribute("principal", "trusted-user");
    }
}
