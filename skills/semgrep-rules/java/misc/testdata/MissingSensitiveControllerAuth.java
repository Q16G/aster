import javax.servlet.http.HttpServletRequest;
import org.springframework.web.bind.annotation.*;
import org.springframework.security.access.prepost.PreAuthorize;

class MissingSensitiveControllerAuth {

    // ruleid: java-misc-missing-checklogin-sensitive-controller
    @PostMapping("/api/admin/createUser")
    public String createUser(String name) {
        return "created";
    }

    // ruleid: java-misc-missing-checklogin-sensitive-controller
    @DeleteMapping("/api/records/deleteRecord")
    public void deleteRecord(String id) {
        // no auth check
    }

    // ok: java-misc-missing-checklogin-sensitive-controller
    @PreAuthorize("hasRole('ADMIN')")
    @PostMapping("/api/admin/createRole")
    public String createRole(String role) {
        return "created";
    }

    // ok: java-misc-missing-checklogin-sensitive-controller
    @PostMapping("/api/admin/updateConfig")
    public String updateConfig(HttpServletRequest request, String key) {
        request.getSession();
        return "updated";
    }

    // ok: java-misc-missing-checklogin-sensitive-controller
    @GetMapping("/api/public/list")
    public String listItems() {
        return "items";
    }
}
