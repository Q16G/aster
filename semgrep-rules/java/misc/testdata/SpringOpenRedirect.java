import javax.servlet.http.HttpServletRequest;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.servlet.ModelAndView;
import org.springframework.web.servlet.view.RedirectView;

@Controller
class SpringOpenRedirectController {
    String insecure(@RequestParam("next") String next) {
        // ruleid: java-misc-open-redirect-spring
        return "redirect:" + next;
    }

    RedirectView insecureView(HttpServletRequest request) {
        // ruleid: java-misc-open-redirect-spring
        return new RedirectView(request.getParameter("url"));
    }

    ModelAndView insecureModel(@RequestParam("target") String target) {
        // ruleid: java-misc-open-redirect-spring
        return new ModelAndView("redirect:" + target);
    }

    String safe() {
        // ok: java-misc-open-redirect-spring
        return "redirect:/dashboard";
    }
}
