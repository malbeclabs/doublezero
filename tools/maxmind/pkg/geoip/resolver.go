package geoip

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
	"github.com/oschwald/geoip2-golang"
)

type Record struct {
	IP          net.IP
	CountryCode string
	Country     string
	Region      string
	City        string
	CityID      int
	Metro       string
	Latitude    float64
	Longitude   float64
	ASN         uint
	ASNOrg      string
}

type Resolver interface {
	Resolve(ip net.IP) *Record
}

type resolver struct {
	log *slog.Logger

	cityDB  *geoip2.Reader
	asnDB   *geoip2.Reader
	metroDB *metrodb.MetroDB
}

func NewResolver(log *slog.Logger, cityDB *geoip2.Reader, asnDB *geoip2.Reader, metroDB *metrodb.MetroDB) (*resolver, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if cityDB == nil {
		return nil, fmt.Errorf("cityDB is nil")
	}
	if asnDB == nil {
		return nil, fmt.Errorf("asnDB is nil")
	}
	if metroDB == nil {
		return nil, fmt.Errorf("metroDB is nil")
	}
	return &resolver{
		log:     log,
		cityDB:  cityDB,
		asnDB:   asnDB,
		metroDB: metroDB,
	}, nil
}

func (r *resolver) Resolve(ip net.IP) *Record {
	if ip == nil {
		return nil
	}

	if r.cityDB == nil && r.asnDB == nil {
		return nil
	}

	var (
		countryCode, country, region, city string
		cityID                             int
		lat, lon                           float64
		asnNum                             uint
		asnOrg                             string
	)

	if r.cityDB != nil {
		rec, err := r.cityDB.City(ip)
		if err != nil {
			r.log.Debug("solana: geoip city lookup failed", "ip", ip.String(), "error", err)
		} else {
			if rec.Country.IsoCode != "" {
				countryCode = rec.Country.IsoCode
			}
			if rec.Country.Names["en"] != "" {
				country = rec.Country.Names["en"]
			}
			if len(rec.Subdivisions) > 0 {
				region = rec.Subdivisions[0].Names["en"]
			}
			if rec.City.GeoNameID != 0 {
				cityID = int(rec.City.GeoNameID)
			}
			if rec.City.Names["en"] != "" {
				city = rec.City.Names["en"]
			}
			lat = rec.Location.Latitude
			lon = rec.Location.Longitude
		}
	}

	if r.asnDB != nil {
		rec, err := r.asnDB.ASN(ip)
		if err != nil {
			r.log.Debug("solana: geoip asn lookup failed", "ip", ip.String(), "error", err)
		} else {
			asnNum = rec.AutonomousSystemNumber
			asnOrg = rec.AutonomousSystemOrganization
		}
	}

	var metro string
	if r.metroDB != nil {
		metro, _ = r.metroDB.Lookup(city, countryCode)
	}
	if metro == "" {
		metro = "Unknown"
	}

	if country == "" && asnNum == 0 {
		return nil
	}

	return &Record{
		IP:          ip,
		CountryCode: countryCode,
		Country:     country,
		Region:      region,
		City:        city,
		CityID:      cityID,
		Metro:       metro,
		Latitude:    lat,
		Longitude:   lon,
		ASN:         asnNum,
		ASNOrg:      asnOrg,
	}
}
