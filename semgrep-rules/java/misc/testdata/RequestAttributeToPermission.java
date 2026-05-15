import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpSession;

class RequestAttributeToPermission {
    void insecure(HttpServletRequest request) {
        // ruleid: java-misc-request-attribute-to-permission-decision
        String role = (String) request.getAttribute("decryptedRole");
        if (role.equals("admin")) {
            performAdminAction();
        }
    }

    void insecureComparison(HttpServletRequest request) {
        // ruleid: java-misc-request-attribute-to-permission-decision
        String userId = (String) request.getAttribute("plainUserId");
        if ("superadmin".equals(userId)) {
            grantAccess();
        }
    }

    void safeSessionBased(HttpServletRequest request) {
        // ok: java-misc-request-attribute-to-permission-decision
        HttpSession session = request.getSession();
        String role = (String) session.getAttribute("role");
        if (role.equals("admin")) {
            performAdminAction();
        }
    }

    private void performAdminAction() {}
    private void grantAccess() {}
}
