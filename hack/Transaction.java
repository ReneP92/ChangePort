import java.io.FileReader;
import java.util.Properties;
import org.apache.kafka.clients.producer.KafkaProducer;
import org.apache.kafka.clients.producer.ProducerRecord;
import org.apache.kafka.common.serialization.StringSerializer;

// Writes a message in a transaction, then commits or aborts it, as a
// transactional producer does: read_committed consumers, and the console's
// tail, see a committed one and never an aborted one. Either way the
// transaction's marker takes the offset after the message. Run through
// hack/kafka-with.sh, with the customer's client.properties.
// Usage: hack/kafka-with.sh <customer> <client.properties> Transaction.java <topic> <value> commit|abort
public class Transaction {
  public static void main(String[] args) throws Exception {
    Properties config = new Properties();
    try (FileReader reader = new FileReader("/config/client.properties")) {
      config.load(reader);
    }
    config.put("transactional.id", "transaction-" + args[0]);
    config.put("key.serializer", StringSerializer.class.getName());
    config.put("value.serializer", StringSerializer.class.getName());
    try (KafkaProducer<String, String> producer = new KafkaProducer<>(config)) {
      producer.initTransactions();
      producer.beginTransaction();
      producer.send(new ProducerRecord<>(args[0], args[1])).get();
      switch (args[2]) {
        case "commit" -> producer.commitTransaction();
        case "abort" -> producer.abortTransaction();
        default -> throw new IllegalArgumentException("commit or abort, not " + args[2]);
      }
    }
  }
}
