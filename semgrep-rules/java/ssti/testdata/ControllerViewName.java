import javax.servlet.http.HttpServletRequest;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.ResponseBody;
import org.springframework.web.servlet.ModelAndView;

@Controller
class ControllerViewName {
    @GetMapping("/page")
    String insecure(@RequestParam("page") String page) {
        // ruleid: java-ssti-controller-view-name
        return page;
    }

    @GetMapping("/theme/{page}")
    String insecurePath(@PathVariable("page") String page) {
        // ruleid: java-ssti-controller-view-name
        return page + ".ftl";
    }

    ModelAndView insecureModel(HttpServletRequest request) {
        String view = request.getParameter("view");
        // ruleid: java-ssti-controller-view-name
        return new ModelAndView(view);
    }

    @ResponseBody
    String safeBody(@RequestParam("page") String page) {
        // ok: java-ssti-controller-view-name
        return page;
    }
}
