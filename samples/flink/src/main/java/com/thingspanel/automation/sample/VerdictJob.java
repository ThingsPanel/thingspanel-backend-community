package com.thingspanel.automation.sample;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.apache.flink.api.common.functions.FilterFunction;
import org.apache.flink.api.common.functions.MapFunction;
import org.apache.flink.configuration.Configuration;
import org.apache.flink.streaming.api.datastream.DataStream;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.flink.streaming.api.functions.sink.RichSinkFunction;

import java.io.Serializable;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * A minimal external executor: it judges one threshold and submits the verdict to
 * ThingsPanel.
 *
 * <p>Everything that makes the platform trust this job lives in the request, not
 * in the job's code:
 *
 * <ul>
 *   <li>{@code epoch} is the registration generation. It is read from the command
 *       line because a job that survived a re-registration must stop, not keep
 *       sending under the old permissions.
 *   <li>{@code seq} must move forward. Two messages with the same number are a
 *       replay and exactly one of them may act.
 *   <li>{@code policy_revision} is the version of the alarm policy this job was
 *       deployed against. A verdict computed from last week's threshold is
 *       refused, which is the whole reason the field exists.
 *   <li>{@code mode=raw} says "the condition is true now". The platform applies
 *       its own published {@code for}. Sending {@code qualified} instead means
 *       "I measured the hold myself", and then {@code since} is mandatory and the
 *       platform recomputes it rather than trusting the label.
 * </ul>
 *
 * <p>Input lines are {@code device,temperature}. Anything else is ignored, so a
 * malformed line cannot turn into a device command.
 */
public final class VerdictJob {

    /** One parsed input line. Public fields + no-arg constructor = a Flink POJO. */
    public static final class Sample implements Serializable {
        public String device;
        public double value;

        public Sample() {}

        Sample(String device, double value) {
            this.device = device;
            this.value = value;
        }
    }

    public static void main(String[] args) throws Exception {
        Map<String, String> cfg = parseArgs(args);
        for (String required : new String[]{"token", "executor-id", "policy-id"}) {
            if (!cfg.containsKey(required)) {
                System.err.println("missing --" + required);
                System.err.println(usage());
                System.exit(2);
            }
        }

        final double threshold = Double.parseDouble(cfg.getOrDefault("threshold", "30"));
        StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
        // One parallel instance, because the sequence number below is a per-sink
        // counter. Scaling out means partitioning by device and keeping a counter
        // per partition — deliberately out of scope for a sample.
        env.setParallelism(1);

        DataStream<String> lines;
        String socket = cfg.get("socket");
        if (socket != null) {
            String[] hp = socket.split(":");
            lines = env.socketTextStream(hp[0], Integer.parseInt(hp[1]));
        } else if (cfg.containsKey("input")) {
            lines = env.readTextFile(cfg.get("input"));
        } else {
            System.err.println("either --input=<file> or --socket=<host:port> is required");
            System.err.println(usage());
            System.exit(2);
            return;
        }

        lines.map(new ParseLine())
                .filter(new Above(threshold))
                .addSink(new VerdictSink(cfg));

        env.execute("thingspanel-verdict-sample");
    }

    /** Parses {@code device,temperature}; returns null for anything else. */
    public static final class ParseLine implements MapFunction<String, Sample> {
        @Override
        public Sample map(String line) {
            String text = line == null ? "" : line.trim();
            if (text.isEmpty() || text.startsWith("#")) {
                return null;
            }
            int comma = text.lastIndexOf(',');
            if (comma <= 0) {
                return null;
            }
            try {
                return new Sample(text.substring(0, comma).trim(),
                        Double.parseDouble(text.substring(comma + 1).trim()));
            } catch (NumberFormatException e) {
                // A sample that cannot be read is not an alarm. Dropping it is the
                // honest choice: guessing a value would invent a verdict.
                return null;
            }
        }
    }

    public static final class Above implements FilterFunction<Sample> {
        private final double threshold;

        Above(double threshold) {
            this.threshold = threshold;
        }

        @Override
        public boolean filter(Sample s) {
            return s != null && s.value > threshold;
        }
    }

    /**
     * Posts one verdict per qualifying sample and prints what the platform said.
     *
     * <p>Printing the refusal is not decoration: {@code denied} carries a
     * {@code fields} list, and resending the same body only produces the same
     * refusal. {@code replayed_sequence} means the message never advanced, so the
     * fix is a new key, not a retry.
     */
    public static final class VerdictSink extends RichSinkFunction<Sample> {
        private static final long serialVersionUID = 1L;

        private final Map<String, String> cfg;
        private transient HttpClient http;
        private transient ObjectMapper json;
        private transient long seq;

        VerdictSink(Map<String, String> cfg) {
            this.cfg = cfg;
        }

        @Override
        public void open(Configuration parameters) {
            http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(5)).build();
            json = new ObjectMapper();
            // The sequence has to keep moving across restarts too, otherwise the
            // first message of a new run looks like a replay of the previous one.
            // A sample uses the clock as a baseline; a real job should persist the
            // counter in Flink state.
            seq = System.currentTimeMillis();
        }

        @Override
        public void invoke(Sample sample, Context context) throws Exception {
            long mySeq = ++seq;
            Instant now = Instant.now();

            Map<String, Object> body = new LinkedHashMap<>();
            body.put("epoch", Long.parseLong(cfg.getOrDefault("epoch", "1")));
            body.put("seq", mySeq);
            body.put("contract_hash", cfg.get("contract-hash"));
            body.put("issued_at", now.toString());
            body.put("expires_at", now.plusSeconds(60).toString());
            body.put("idempotency_key", "flink-" + cfg.get("executor-id") + "-" + mySeq);
            body.put("policy_id", cfg.get("policy-id"));
            body.put("policy_revision", Long.parseLong(cfg.getOrDefault("policy-revision", "1")));
            body.put("mode", "raw");
            body.put("observed", true);
            body.put("subject_id", sample.device);
            body.put("action_kind", "qualified");

            String base = cfg.getOrDefault("base", "http://127.0.0.1:14000").replaceAll("/+$", "");
            HttpRequest request = HttpRequest.newBuilder(
                            URI.create(base + "/v1/executors/" + cfg.get("executor-id") + "/verdicts"))
                    .header("authorization", "Bearer " + cfg.get("token"))
                    .header("content-type", "application/json")
                    .timeout(Duration.ofSeconds(10))
                    .POST(HttpRequest.BodyPublishers.ofString(json.writeValueAsString(body)))
                    .build();

            HttpResponse<String> response = http.send(request, HttpResponse.BodyHandlers.ofString());
            JsonNode node = null;
            if (response.body() != null && !response.body().isEmpty()) {
                try {
                    node = json.readTree(response.body());
                } catch (Exception ignored) {
                    // Fall through: an unparsable body is reported as an HTTP code.
                }
            }

            if (node != null && node.path("accepted").asBoolean(false)) {
                System.out.printf("[verdict] seq=%d %s %.1f -> %s%s%n", mySeq, sample.device, sample.value,
                        node.path("outcome").asText("accepted"),
                        node.path("replayed").asBoolean(false) ? " (replayed, nothing ran twice)" : "");
                return;
            }

            String reason = node == null ? "http_" + response.statusCode() : node.path("reason_code").asText("denied");
            System.out.printf("[verdict] seq=%d %s %.1f -> REFUSED %s%n", mySeq, sample.device, sample.value, reason);
            if (node != null) {
                for (JsonNode issue : node.path("issues")) {
                    System.out.println("           " + issue.path("field").asText() + ": " + issue.path("message").asText());
                }
            }
            System.out.println("           不要原样重试：按上面的字段改请求；序号没有前进时换新 key。");
        }
    }

    private static Map<String, String> parseArgs(String[] args) {
        Map<String, String> cfg = new LinkedHashMap<>();
        for (String arg : args) {
            if (!arg.startsWith("--")) {
                continue;
            }
            int eq = arg.indexOf('=');
            if (eq < 0) {
                cfg.put(arg.substring(2), "");
            } else {
                cfg.put(arg.substring(2, eq), arg.substring(eq + 1));
            }
        }
        return cfg;
    }

    private static String usage() {
        return "usage: --base=<url> --token=<bearer> --executor-id=<id> --contract-hash=<hash> "
                + "--epoch=<n> --policy-id=<id> --policy-revision=<n> --threshold=<t> "
                + "(--input=<file> | --socket=<host:port>)";
    }

    private VerdictJob() {}
}
