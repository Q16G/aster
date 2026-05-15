import groovy.lang.GroovyClassLoader;
import groovy.lang.GroovyShell;

class GroovyShellInjection {
    void insecure(String script) {
        GroovyShell shell = new GroovyShell();
        // ruleid: java-injection-code-injection-groovy-shell
        shell.evaluate(script);
        GroovyClassLoader loader = new GroovyClassLoader();
        // ruleid: java-injection-code-injection-groovy-shell
        loader.parseClass(script);
    }

    void safe() {
        GroovyShell shell = new GroovyShell();
        // ok: java-injection-code-injection-groovy-shell
        shell.evaluate("println 'trusted'");
    }
}
