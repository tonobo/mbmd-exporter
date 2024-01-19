require 'sinatra'
require 'typhoeus'
require 'oj'
require 'time'
require 'mqtt'
require 'logger'
require 'time'

$LOGGER = Logger.new(STDERR)

URL = ENV.fetch('URL')

if ENV["ENABLE_VICTRON_GRID_MQTT"]
  SLEEP_TIME = 5 

  Thread.new do
    client = MQTT::Client.connect(ENV.fetch("MQTT_HOST"))

    loop do
      start = Time.now
      response = Typhoeus::Request.new("#{URL}/api/last", timeout: 3).run
      unless response.success?
        raise "failed to contact mbmd: #{response}" 
      end
      json = Oj.safe_load(response.body)
      json.each do |device_id, values|
        topic = "mbmd-exporter/#{device_id}/metrics".downcase
        data = {
          grid: {
            voltage: values.fetch("VoltageL1", 0),
            power: values.fetch("PowerL1", 0) +
            values.fetch("PowerL2", 0) + 
            values.fetch("PowerL3", 0),
            current: values.fetch("CurrentL1", 0) +
            values.fetch("CurrentL2", 0) + 
            values.fetch("CurrentL3", 0),
            energy_forward: values.fetch("Export", 0),
            energy_reverse: values.fetch("Import", 0),
            L1: {
              power: values.fetch("PowerL1", 0),
              voltage: values.fetch("VoltageL1", 0),
              current: values.fetch("CurrentL1", 0),
              energy_forward: values.fetch("ExportL1", 0),
              energy_reverse: values.fetch("ImportL1", 0),
            },
            L2: {
              power: values.fetch("PowerL2", 0),
              voltage: values.fetch("VoltageL2", 0),
              current: values.fetch("CurrentL2", 0),
              energy_forward: values.fetch("ExportL2", 0),
              energy_reverse: values.fetch("ImportL2", 0),
            },
            L3: {
              power: values.fetch("PowerL3", 0),
              voltage: values.fetch("VoltageL3", 0),
              current: values.fetch("CurrentL3", 0),
              energy_forward: values.fetch("ExportL3", 0),
              energy_reverse: values.fetch("ImportL3", 0),
            }
          }
        }
        client.publish(topic, Oj.dump(data, mode: :compat))
      end
      duration = Time.now - start
      $LOGGER.info "Data collection took #{duration.to_f.round(3)} seconds"
      sleep(SLEEP_TIME - duration)
    end
  rescue StandardError => error
    $LOGGER.error error
  ensure
    # exit programm in case of failures
    exit(1)
  end
end

if ENV["ENABLE_HASS_DISCOVERY"]
  # ENV MQTT_HOST required
  Thread.new do
    cache = {}
    lookup_table = {
      Export:         { unit: :Wh,  t: :total_increasing, c: :energy },
      Import:         { unit: :Wh,  t: :total_increasing, c: :energy },
      Sum:            { unit: :kWh, t: :total_increasing, c: :energy },
      Cosphi:         {             t: :measurement,      c: :power_factor },
      Current:        { unit: :A,   t: :measurement,      c: :current },
      Voltage:        { unit: :V,   t: :measurement,      c: :voltage },
      Power:          { unit: :W,   t: :measurement,      c: :power },
      ReactivePower:  { unit: :var, t: :measurement,      c: :reactive_power },
      Frequency:      { unit: :Hz,  t: :measurement,      c: :frequency },
    }

    client = MQTT::Client.connect(ENV.fetch("MQTT_HOST"))
    client.subscribe(ENV.fetch("MQTT_TOPIC", "mbmd/#"))
    client.get do |topic, message|
      _prefix, device, kind, phase, _other = *topic.split("/")
      kind = kind.to_s.to_sym
      raise "_other filled, unsupported message -> topic: #{topic}, message: #{message}" if _other
      next if device == "status" # skip status message
      next if phase.to_s.match?(/^T\d+$/) # skip tarif counter
      next unless lookup_table[kind]
      
      data = {
        name: [kind, (phase || "Total")].join(" "),
        stat_t: topic,
        uniq_id: "mbmd-exporter-bridge_#{device.gsub(".","-")}_#{[kind, phase].compact.join("-").downcase}",
        dev_cla: lookup_table.dig(kind, :c), 
        stat_cla: lookup_table.dig(kind, :t),
        exp_aft: 10,
        dev: { name: device, ids: device.gsub(".","-"), cu: URL, mf: "mbmd", mdl: device },
      }

      next if cache[data[:uniq_id]] # already published, skipping
      cache[data[:uniq_id]] = true

      data[:unit_of_meas] = lookup_table.dig(kind, :unit) if lookup_table.dig(kind, :unit) 
      key = "homeassistant/sensor/mbmd-exporter-bridge-"\
        "#{device.gsub(".", "-")}/#{[kind, phase].compact.join("-").downcase}/config"
      client.publish(key, Oj.dump(data, mode: :compat), true)
    end
  end
end

get '/metrics' do
  response = Typhoeus::Request.new("#{URL}/api/last", timeout: 1).run
  unless response.success?
    p response
    raise "failed" 
  end

  json = Oj.safe_load(response.body)

  metrics_gauge = []
  metrics_counter = []

  json.each do |device_id, values|
    time = Time.parse(values.delete('Timestamp')).to_f rescue nil
    time = nil
    {
      'Frequency' => :frequency_hz,
      'PowerL1' => :power_l1_watts,
      'PowerL2' => :power_l2_watts,
      'PowerL3' => :power_l3_watts,
      'Power' => :power_watts,
      'ApparentPower' => :apparent_power_watts,
      'VoltageL1' => :voltage_l1_volts,
      'VoltageL2' => :voltage_l2_volts,
      'VoltageL3' => :voltage_l3_volts,
      'CurrentL1' => :current_l1_amps,
      'CurrentL2' => :current_l2_amps,
      'CurrentL3' => :current_l3_amps,
    }.each do |key, metric_name|
      value = values.fetch(key)
      metrics_gauge << %(#TYPE mbmd_#{metric_name} gauge)
      metrics_gauge << %(mbmd_#{metric_name}{device="#{device_id}"} #{value.to_f} #{time}).strip
    rescue KeyError
    end
    
    {
      'SumL1' => :total_l1_kwh,
      'SumL2' => :total_l2_kwh,
      'SumL3' => :total_l3_kwh,
      'Sum' => :total_kwh,
      'ImportL1' => :import_l1_kwh,
      'ImportL2' => :import_l2_kwh,
      'ImportL3' => :import_l3_kwh,
      'Import' =>   :import_kwh,
      'ExportL1' => :export_l1_kwh,
      'ExportL2' => :export_l2_kwh,
      'ExportL3' => :export_l3_kwh,
      'Export' =>   :export_kwh,
    }.each do |key, metric_name|
      value = values.fetch(key)
      metrics_counter << %(#TYPE mbmd_#{metric_name} counter)
      metrics_counter << %(mbmd_#{metric_name}{device="#{device_id}"} #{value.to_f} #{time}).strip
    rescue KeyError
    end
  end

  headers 'Content-Type' => 'text/plain'

  [metrics_gauge, metrics_counter].flatten.join("\n") + "\n"
end
