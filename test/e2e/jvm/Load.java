import com.sun.net.httpserver.HttpServer;
import java.lang.management.ManagementFactory;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.Executors;

// Real JVM heap and non-heap observations, exposed without third-party agents.
public class Load {
    static final byte[] heap = new byte[64 * 1024 * 1024];
    public static void main(String[] args) throws Exception {
        for (int i = 0; i < heap.length; i += 4096) heap[i] = 1;
        var memory = ManagementFactory.getMemoryMXBean();
        var server = HttpServer.create(new InetSocketAddress(8080), 0);
        server.setExecutor(Executors.newFixedThreadPool(2));
        server.createContext("/metrics", exchange -> {
            String body = "# TYPE optipod_jvm_heap_bytes gauge\noptipod_jvm_heap_bytes " + memory.getHeapMemoryUsage().getUsed()
                + "\n# TYPE optipod_jvm_max_bytes gauge\noptipod_jvm_max_bytes " + memory.getHeapMemoryUsage().getMax()
                + "\n# TYPE optipod_jvm_nonheap_bytes gauge\noptipod_jvm_nonheap_bytes " + memory.getNonHeapMemoryUsage().getUsed()
                + "\n# TYPE app_cpu_demand_cores gauge\napp_cpu_demand_cores 0.35\n";
            byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().set("Content-Type", "text/plain; version=0.0.4");
            exchange.sendResponseHeaders(200, bytes.length);
            exchange.getResponseBody().write(bytes); exchange.close();
        });
        server.createContext("/ready", exchange -> { exchange.sendResponseHeaders(200, 0); exchange.close(); });
        server.start();
    }
}
