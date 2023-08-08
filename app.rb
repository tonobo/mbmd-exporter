require 'sinatra'
require 'typhoeus'
require 'oj'
require 'time'

URL = ENV.fetch('URL')

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
