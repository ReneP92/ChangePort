package io.changeport.sample;

import java.time.Duration;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Properties;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ThreadLocalRandom;
import org.apache.kafka.clients.consumer.ConsumerRecord;
import org.apache.kafka.clients.consumer.KafkaConsumer;
import org.apache.kafka.clients.producer.KafkaProducer;
import org.apache.kafka.clients.producer.ProducerRecord;
import org.apache.kafka.common.errors.TimeoutException;
import org.apache.kafka.common.errors.WakeupException;
import org.apache.kafka.common.serialization.StringDeserializer;
import org.apache.kafka.common.serialization.StringSerializer;

/**
 * The sample app of the console's guided first ten minutes: a coffee shop
 * whose checkout writes an order event to a topic a few times a second, keyed
 * by order ID, and whose email service reads them as a consumer group and
 * sends each customer a confirmation, as two applications of a customer would.
 * It connects as the sample KafkaUser with a password, inside the customer's
 * cluster, and reports its metrics to the brokers (KIP-714), so the console's
 * data flow map shows it.
 *
 * <p>It reads its settings from the environment: KAFKA_BOOTSTRAP,
 * KAFKA_SASL_JAAS_CONFIG, KAFKA_TRUSTSTORE (the cluster CA's PKCS12 file),
 * KAFKA_TRUSTSTORE_PASSWORD, TOPIC and GROUP.
 */
public final class SampleShop {
    private static final List<String> CUSTOMERS = List.of(
            "Mira Castell", "Jonas Berg", "Priya Natarajan", "Tomás Ruiz", "Aiko Tanaka",
            "Lena Vogel", "Samuel Okafor", "Elif Demir", "Noah Lindqvist", "Chloé Martin");

    private record Product(String name, int cents) {}

    private static final List<Product> PRODUCTS = List.of(
            new Product("Espresso beans, 250 g", 1290),
            new Product("Oat milk, 1 l", 249),
            new Product("Pour-over kettle", 4900),
            new Product("Ceramic mug", 1450),
            new Product("Paper filters, 100", 590),
            new Product("Decaf beans, 250 g", 1190),
            new Product("Hand grinder", 6900));

    private SampleShop() {}

    public static void main(String[] args) throws InterruptedException {
        Map<String, String> env = System.getenv();
        String topic = require(env, "TOPIC");
        String group = require(env, "GROUP");
        Properties connection = new Properties();
        connection.put("bootstrap.servers", require(env, "KAFKA_BOOTSTRAP"));
        connection.put("security.protocol", "SASL_SSL");
        connection.put("sasl.mechanism", "SCRAM-SHA-512");
        connection.put("sasl.jaas.config", require(env, "KAFKA_SASL_JAAS_CONFIG"));
        connection.put("ssl.truststore.type", "PKCS12");
        connection.put("ssl.truststore.location", require(env, "KAFKA_TRUSTSTORE"));
        connection.put("ssl.truststore.password", require(env, "KAFKA_TRUSTSTORE_PASSWORD"));

        CountDownLatch stopped = new CountDownLatch(2);
        Checkout checkout = new Checkout(connection, topic, stopped);
        Emails emails = new Emails(connection, topic, group, stopped);
        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            checkout.stop();
            emails.stop();
            try {
                stopped.await();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }));
        Thread.ofPlatform().name("checkout").start(checkout);
        Thread.ofPlatform().name("emails").start(emails);
        stopped.await();
    }

    /**
     * Ends the app when a part of it ends without being stopped, as when its
     * client fails, for Kubernetes to start it again.
     */
    private static void exitUnlessStopped(boolean running, String part) {
        if (running) {
            System.err.println("The " + part + " ended; exiting");
            System.exit(1);
        }
    }

    private static String require(Map<String, String> env, String name) {
        String value = env.get(name);
        if (value == null || value.isEmpty()) {
            throw new IllegalArgumentException(name + " is not set");
        }
        return value;
    }

    /** The shop's checkout: it writes an order event for every order placed. */
    private static final class Checkout implements Runnable {
        private final Properties config = new Properties();
        private final String topic;
        private final CountDownLatch stopped;
        private volatile boolean running = true;

        Checkout(Properties connection, String topic, CountDownLatch stopped) {
            config.putAll(connection);
            config.put("client.id", "sample-shop");
            config.put("key.serializer", StringSerializer.class.getName());
            config.put("value.serializer", StringSerializer.class.getName());
            // The defaults that matter: every replica in sync has the order
            // before it counts as sent (acks=all), and retries never write it
            // twice (idempotence).
            config.put("acks", "all");
            config.put("enable.idempotence", "true");
            // Waits for the topic's metadata end within the pod's grace
            // period, so that the app stops when asked.
            config.put("max.block.ms", "10000");
            this.topic = topic;
            this.stopped = stopped;
        }

        void stop() {
            running = false;
        }

        @Override
        public void run() {
            ThreadLocalRandom random = ThreadLocalRandom.current();
            long order = random.nextLong(100_000, 900_000);
            KafkaProducer<String, String> producer = null;
            try {
                producer = new KafkaProducer<>(config);
                // The topic may not be there yet, as it is created when the
                // app is: orders wait for it rather than fail.
                while (running && !topicExists(producer)) {
                    Thread.sleep(1_000);
                }
                while (running) {
                    String id = "ord-" + order++;
                    producer.send(new ProducerRecord<>(topic, id, orderEvent(id, random)), (metadata, error) -> {
                        if (error != null) {
                            System.err.println("Could not write order " + id + ": " + error.getMessage());
                        }
                    });
                    // Three to six orders a second, as customers come.
                    Thread.sleep(random.nextLong(160, 340));
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            } catch (RuntimeException e) {
                e.printStackTrace();
            } finally {
                // Orders still on their way get a few seconds, as the sample
                // stops with its topic.
                if (producer != null) {
                    producer.close(Duration.ofSeconds(5));
                }
                stopped.countDown();
                exitUnlessStopped(running, "checkout");
            }
        }

        private boolean topicExists(KafkaProducer<String, String> producer) {
            try {
                return !producer.partitionsFor(topic).isEmpty();
            } catch (TimeoutException e) {
                return false;
            }
        }

        private static String orderEvent(String id, ThreadLocalRandom random) {
            StringBuilder items = new StringBuilder();
            int total = 0;
            int lines = random.nextInt(1, 4);
            for (int i = 0; i < lines; i++) {
                Product product = PRODUCTS.get(random.nextInt(PRODUCTS.size()));
                int quantity = random.nextInt(1, 3);
                total += product.cents() * quantity;
                if (i > 0) {
                    items.append(',');
                }
                items.append(String.format(Locale.ROOT, "{\"product\":\"%s\",\"quantity\":%d,\"price\":\"%s\"}",
                        product.name(), quantity, euros(product.cents())));
            }
            return String.format(Locale.ROOT,
                    "{\"orderId\":\"%s\",\"customer\":\"%s\",\"items\":[%s],\"total\":\"%s\",\"currency\":\"EUR\",\"placedAt\":\"%s\"}",
                    id, CUSTOMERS.get(random.nextInt(CUSTOMERS.size())), items, euros(total),
                    Instant.now().truncatedTo(ChronoUnit.MILLIS));
        }

        private static String euros(int cents) {
            return String.format(Locale.ROOT, "%d.%02d", cents / 100, cents % 100);
        }
    }

    /** The shop's email service: it confirms every order it reads. */
    private static final class Emails implements Runnable {
        private final Properties config = new Properties();
        private final String topic;
        private final CountDownLatch stopped;
        private volatile KafkaConsumer<String, String> consumer;
        private volatile boolean running = true;

        Emails(Properties connection, String topic, String group, CountDownLatch stopped) {
            config.putAll(connection);
            config.put("client.id", "sample-order-emails");
            config.put("group.id", group);
            config.put("auto.offset.reset", "earliest");
            config.put("key.deserializer", StringDeserializer.class.getName());
            config.put("value.deserializer", StringDeserializer.class.getName());
            this.topic = topic;
            this.stopped = stopped;
        }

        void stop() {
            running = false;
            KafkaConsumer<String, String> current = consumer;
            if (current != null) {
                current.wakeup();
            }
        }

        @Override
        public void run() {
            try (KafkaConsumer<String, String> consumer = new KafkaConsumer<>(config)) {
                this.consumer = consumer;
                consumer.subscribe(List.of(topic));
                long sent = 0;
                while (running) {
                    for (ConsumerRecord<String, String> record : consumer.poll(Duration.ofSeconds(1))) {
                        if (++sent % 100 == 0) {
                            System.out.println("Sent " + sent + " confirmation emails, the last for order " + record.key());
                        }
                    }
                }
            } catch (WakeupException e) {
                // Stopping.
            } catch (RuntimeException e) {
                e.printStackTrace();
            } finally {
                stopped.countDown();
                exitUnlessStopped(running, "email service");
            }
        }
    }
}
